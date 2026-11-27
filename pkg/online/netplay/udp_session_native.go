//go:build !js

package netplay

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	stdnet "net"
	"slices"
	"sync"
	"time"
)

const (
	udpHandshakeTimeout = 10 * time.Second
	udpHelloRetry       = 250 * time.Millisecond
	udpKeepAlive        = time.Second
	udpClientIdle       = 10 * time.Second
)

// udpLink binds a UDP session to the address and directional AEAD ciphers established by its Noise NK handshake.
type udpLink struct {
	addr       stdnet.Addr
	nonce      [hsNonceSize]byte
	helloHash  [sha256.Size]byte
	welcome    []byte
	sendSeq    uint32
	sendCipher udpCipher
	recvCipher udpCipher
	recv       replayWindow
}

// UDPServer is a native UDP Transport for a server whose clients connect with DialUDP. Each client runs a Noise NK key exchange (handshake version 2) against the server's static X25519 key before its packets reach Recv: the token and resume secret travel encrypted, Auth.Verify checks the token, and the server assigns the client's ID in an encrypted welcome.
//
// After the handshake every datagram is encrypted and authenticated with ChaCha20-Poly1305 using directional keys from the Noise split, a 10-byte cleartext header authenticated as associated data, an implicit direction||peer||seq nonce, and a 64-entry replay window that also refuses wrapped sequence numbers. A datagram from another address, with a bad tag, reflected from the opposite direction, or replayed is dropped.
//
// UDP has no connection, so a client that stays silent for Auth.IdleTimeout counts as dropped; DialUDP clients ping every second while idle.
type UDPServer struct {
	conn      stdnet.PacketConn
	serverID  PeerID
	staticKey *ecdh.PrivateKey

	mu      sync.Mutex
	table   *sessionTable
	byNonce map[[hsNonceSize]byte]PeerID
	inbox   packetQueue
	closed  bool
	err     error
	changed chan struct{}
}

// ListenUDPServer binds a UDP socket on addr for a server with ID serverID (1 when zero) and starts accepting DialUDP clients under auth. If auth.StaticKey is nil, ListenUDPServer generates a fresh X25519 key pair whose public key clients obtain from PublicKey; otherwise auth.StaticKey must use X25519 or ListenUDPServer returns ErrInvalidKey.
func ListenUDPServer(addr string, serverID PeerID, auth Auth) (*UDPServer, error) {
	if serverID == 0 {
		serverID = 1
	}
	staticKey, err := resolveUDPServerKey(auth.StaticKey)
	if err != nil {
		return nil, err
	}
	conn, err := stdnet.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	return newUDPServerWithKey(conn, serverID, auth, staticKey), nil
}

func resolveUDPServerKey(k *ecdh.PrivateKey) (*ecdh.PrivateKey, error) {
	if k == nil {
		return ecdh.X25519().GenerateKey(rand.Reader)
	}
	if err := validateX25519PrivateKey(k); err != nil {
		return nil, err
	}
	return k, nil
}

func newUDPServer(conn stdnet.PacketConn, serverID PeerID, auth Auth) *UDPServer {
	staticKey, err := resolveUDPServerKey(auth.StaticKey)
	if err != nil {
		panic(err)
	}
	return newUDPServerWithKey(conn, serverID, auth, staticKey)
}

func newUDPServerWithKey(conn stdnet.PacketConn, serverID PeerID, auth Auth, staticKey *ecdh.PrivateKey) *UDPServer {
	s := &UDPServer{
		conn:      conn,
		serverID:  serverID,
		staticKey: staticKey,
		table:     newSessionTable(auth, serverID, true),
		byNonce:   make(map[[hsNonceSize]byte]PeerID),
		changed:   make(chan struct{}, 1),
	}
	go s.readLoop()
	return s
}

// Addr returns the local bound UDP address.
func (s *UDPServer) Addr() stdnet.Addr {
	return s.conn.LocalAddr()
}

// PublicKey returns the server's static X25519 public key that clients pass to DialUDP.
func (s *UDPServer) PublicKey() *ecdh.PublicKey {
	return s.staticKey.PublicKey()
}

func (s *UDPServer) readLoop() {
	buf := make([]byte, 65535)
	plainBuf := make([]byte, 0, 65535)
	for {
		n, from, err := s.conn.ReadFrom(buf)
		if err != nil {
			if udpReadRetry(err) {
				continue
			}
			s.mu.Lock()
			if !s.closed {
				s.err = udpReadError(err)
			}
			s.mu.Unlock()
			s.notify()
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
			s.handleSealed(b, from, plainBuf)
		}
	}
}

func (s *UDPServer) handleHello(b []byte, from stdnet.Addr) {
	if len(b) < udpMinHello {
		return
	}
	if b[1] != udpHandshakeVersion {
		var nonce [hsNonceSize]byte
		copy(nonce[:], b[2:udpHelloPrefixSize])
		_, _ = s.conn.WriteTo(encodeReject(nonce, hsReasonVersion, len(b)), from)
		return
	}
	var nonce [hsNonceSize]byte
	copy(nonce[:], b[2:udpHelloPrefixSize])
	helloHash := sha256.Sum256(b)

	// A retransmitted hello whose welcome was lost gets the same encrypted welcome again without repeating the DH or creating a second session.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if id, ok := s.byNonce[nonce]; ok {
		if sess := s.table.sessions[id]; sess != nil {
			if link, ok := sess.link.(*udpLink); ok && link.nonce == nonce && link.helloHash == helloHash && sameUDPAddr(link.addr, from) {
				reply := link.welcome
				s.mu.Unlock()
				_, _ = s.conn.WriteTo(reply, from)
				return
			}
		}
	}
	s.mu.Unlock()

	resp, h, err := decodeUDPHelloV2(s.staticKey, b)
	if err != nil {
		return
	}
	if reply := s.admit(resp, h, helloHash, from, len(b)); reply != nil {
		_, _ = s.conn.WriteTo(reply, from)
	}
}

// admit runs the session side of the handshake and returns the encrypted Noise message 2 reply.
func (s *UDPServer) admit(resp *noiseNKResponder, h hello, helloHash [sha256.Size]byte, from stdnet.Addr, limit int) []byte {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	now := s.table.now()
	s.table.expire(now)

	if id, ok := s.byNonce[h.nonce]; ok {
		if sess := s.table.sessions[id]; sess != nil {
			if link, ok := sess.link.(*udpLink); ok && link.nonce == h.nonce && link.helloHash == helloHash && sameUDPAddr(link.addr, from) {
				s.mu.Unlock()
				return link.welcome
			}
		}
		delete(s.byNonce, h.nonce)
	}

	if sess := s.table.resume(h, now); sess != nil {
		reply := s.bindLocked(resp, sess, h.nonce, helloHash, from, true)
		s.mu.Unlock()
		s.notify()
		return reply
	}
	id, ok := s.table.reserve()
	s.mu.Unlock()
	if !ok {
		reply, _ := encodeUDPRejectV2(resp, h.nonce, reasonServerFull, limit)
		return reply
	}

	reason, ok := s.table.verify(id, h.token)

	s.mu.Lock()
	if !ok || s.closed {
		s.table.release(id)
		s.mu.Unlock()
		if !ok {
			reply, _ := encodeUDPRejectV2(resp, h.nonce, reason, limit)
			return reply
		}
		return nil
	}
	sess := s.table.commit(id, s.table.now())
	reply := s.bindLocked(resp, sess, h.nonce, helloHash, from, false)
	s.mu.Unlock()
	s.notify()
	return reply
}

// bindLocked completes the Noise NK handshake for sess, binds it to from with fresh directional AEAD ciphers and a fresh replay window, and records its welcome for retransmits.
func (s *UDPServer) bindLocked(resp *noiseNKResponder, sess *session, nonce [hsNonceSize]byte, helloHash [sha256.Size]byte, from stdnet.Addr, resumed bool) []byte {
	if old, ok := sess.link.(*udpLink); ok {
		delete(s.byNonce, old.nonce)
	}
	reply, c2sKey, s2cKey, err := encodeUDPWelcomeV2(resp, s.table.welcomeFor(sess, nonce, resumed))
	if err != nil {
		return nil
	}
	sendCipher := newUDPCipher(s2cKey)
	recvCipher := newUDPCipher(c2sKey)
	clear(c2sKey[:])
	clear(s2cKey[:])
	sess.link = &udpLink{
		addr:       from,
		nonce:      nonce,
		helloHash:  helloHash,
		welcome:    reply,
		sendCipher: sendCipher,
		recvCipher: recvCipher,
	}
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

func (s *UDPServer) handleSealed(b []byte, from stdnet.Addr, plainBuf []byte) {
	hdr, ok := parseSealed(b)
	if !ok {
		return
	}
	s.mu.Lock()
	sess := s.table.sessions[hdr.peer]
	link, linked := linkOf(sess)
	if s.closed || !linked || !sess.connected || !sameUDPAddr(link.addr, from) || !link.recv.wouldAccept(hdr.seq) {
		s.mu.Unlock()
		return
	}
	pt, valid := link.recvCipher.open(plainBuf, udpDirClientToServer, b)
	if !valid {
		s.mu.Unlock()
		return
	}
	link.recv.accept(hdr.seq)
	sess.lastSeen = s.table.now()
	var reply []byte
	switch hdr.typ {
	case udpData:
		if hdr.ch == ChannelUnreliable {
			s.inbox.push(Packet{
				From:    hdr.peer,
				Channel: hdr.ch,
				Payload: slices.Clone(pt),
				Seq:     hdr.seq,
			})
		}
	case udpPing:
		if seq, ok := nextSeq(&link.sendSeq); ok {
			reply = link.sendCipher.seal(nil, udpDirServerToClient, udpPong, sess.id, seq, ChannelUnreliable, nil)
		}
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

// Send encrypts and authenticates payload with the client's server-to-client ChaCha20-Poly1305 key and sends it in one datagram to the client (or all connected clients when to == PeerBroadcast). It refuses ChannelReliableOrdered with ErrUnsupportedChannel, returns ErrDisconnected for a client inside its grace period, ErrUnknownPeer for an ID with no session, and ErrSequenceExhausted if the session's 32-bit sequence counter would wrap.
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
	if s.err != nil {
		err := s.err
		s.mu.Unlock()
		return err
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
	var firstErr error
	targets := make([]target, 0, len(ids))
	for _, id := range ids {
		sess := s.table.sessions[id]
		link := sess.link.(*udpLink)
		seq, ok := nextSeq(&link.sendSeq)
		if !ok {
			if to != PeerBroadcast {
				s.mu.Unlock()
				return ErrSequenceExhausted
			}
			if firstErr == nil {
				firstErr = ErrSequenceExhausted
			}
			continue
		}
		targets = append(targets, target{addr: link.addr, dgram: link.sendCipher.seal(nil, udpDirServerToClient, udpServerData, id, seq, ch, payload)})
	}
	s.mu.Unlock()

	for _, t := range targets {
		if _, err := s.conn.WriteTo(t.dgram, t.addr); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SetInboxLimits bounds the packets the server keeps until Recv takes them (see InboxLimits).
func (s *UDPServer) SetInboxLimits(l InboxLimits) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inbox.setLimits(l)
}

// InboxStats reports what the server's inbox holds and what its limits dropped or warned about.
func (s *UDPServer) InboxStats() InboxStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inbox.stats()
}

// Recv pops the next authenticated and decrypted packet without blocking.
func (s *UDPServer) Recv() (Packet, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inbox.pop()
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

// Err returns why the server stopped receiving: ErrReceiveFailed wrapping the socket's error. Send returns the same error from then on, and clients time out as their datagrams go unread, so a game should close the server and listen again. It is nil while the server receives, and after Close. Errors about a single datagram, such as an ICMP port unreachable for an earlier send, do not stop it.
func (s *UDPServer) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
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
	s.err = nil
	s.mu.Unlock()
	return s.conn.Close()
}

// UDPClient is a native UDP Transport connected to a UDPServer. DialUDP authenticates the server and derives directional ChaCha20-Poly1305 session keys using a Noise NK handshake against the server's static X25519 public key. The client pings the server while idle and reports itself disconnected when the server stays silent for 10 seconds; Reconnect runs a fresh Noise NK handshake to rekey the session and resume with the same ID within the server's grace period.
type UDPClient struct {
	addr             string
	serverKey        *ecdh.PublicKey
	token            []byte
	handshakeTimeout time.Duration

	dialMu sync.Mutex // serializes Reconnect

	mu         sync.Mutex
	conn       stdnet.Conn
	stop       chan struct{}
	localID    PeerID
	serverID   PeerID
	sendCipher udpCipher
	recvCipher udpCipher
	sendSeq    uint32
	recv       replayWindow
	lastSend   time.Time
	lastRecv   time.Time
	state      ConnState
	err        error
	resume     resumeState
	resumed    bool
	inbox      packetQueue
	changed    chan struct{}

	// admissions counts handshakes that admitted the client as a new peer rather than resuming, so a RelayTransport can tell its relay session was lost even when the new ID equals the old one.
	admissions uint64
}

// DialUDP connects to the UDPServer at addr, authenticates it with its static X25519 public key serverKey using Noise NK, presents token encrypted in the handshake, and returns once the server accepts it. The server assigns the client's ID, which LocalID reports. A missing or non-X25519 serverKey returns ErrInvalidKey; a refusal returns a *RejectError that matches ErrRejected.
func DialUDP(addr string, serverKey *ecdh.PublicKey, token []byte) (*UDPClient, error) {
	return dialUDP(addr, serverKey, token, udpHandshakeTimeout)
}

func dialUDP(addr string, serverKey *ecdh.PublicKey, token []byte, timeout time.Duration) (*UDPClient, error) {
	if err := validateX25519PublicKey(serverKey); err != nil {
		return nil, err
	}
	if len(token) > MaxTokenSize {
		return nil, ErrTokenTooLarge
	}
	if timeout <= 0 {
		timeout = udpHandshakeTimeout
	}
	c := &UDPClient{
		addr:             addr,
		serverKey:        serverKey,
		token:            slices.Clone(token),
		handshakeTimeout: timeout,
		changed:          make(chan struct{}, 1),
	}
	if err := c.connect(); err != nil {
		return nil, err
	}
	return c, nil
}

// Reconnect drops the current socket without a goodbye, opens a new one and runs a fresh Noise NK handshake to rekey the session and reset sequence numbers: within the server's grace period the client keeps its ID, and Resumed reports true. Past it, the server admits the client as a new peer when its token is still valid, and LocalID changes.
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
	w, c2sKey, s2cKey, err := udpHandshake(conn, c.serverKey, c.token, res, c.handshakeTimeout)
	if err != nil {
		_ = conn.Close()
		return c.fail(err)
	}
	_ = conn.SetReadDeadline(time.Time{})

	sendCipher := newUDPCipher(c2sKey)
	recvCipher := newUDPCipher(s2cKey)
	clear(c2sKey[:])
	clear(s2cKey[:])

	c.mu.Lock()
	if c.state == ConnClosed {
		c.mu.Unlock()
		_ = conn.Close()
		return ErrClosed
	}
	now := time.Now()
	stop := make(chan struct{})
	c.conn, c.stop = conn, stop
	c.localID, c.serverID = w.peer, w.server
	c.sendCipher, c.recvCipher = sendCipher, recvCipher
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

// udpHandshake sends the Noise NK hello, resending the identical datagram until the server answers with a valid welcome or reject or the handshake times out.
func udpHandshake(conn stdnet.Conn, serverKey *ecdh.PublicKey, token []byte, res resumeState, timeout time.Duration) (w welcome, c2sKey, s2cKey [32]byte, err error) {
	h, err := newHello(token, res)
	if err != nil {
		return w, c2sKey, s2cKey, err
	}
	dgram, init, err := encodeUDPHelloV2(serverKey, h)
	if err != nil {
		return w, c2sKey, s2cKey, err
	}
	if timeout <= 0 {
		timeout = udpHandshakeTimeout
	}
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 2048)
	retry := min(udpHelloRetry, timeout)
	for {
		if _, err := conn.Write(dgram); err != nil {
			return w, c2sKey, s2cKey, err
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
				return w, c2sKey, s2cKey, err
			}
			if n == 0 || (buf[0] != hsWelcome && buf[0] != hsReject) {
				continue
			}
			w, c2sKey, s2cKey, err = acceptUDPServerHandshakeV2(init, h.nonce, buf[:n])
			if errors.Is(err, ErrInvalidPacket) {
				continue // a late reply to an earlier dial, or an unauthenticated/forged datagram
			}
			return w, c2sKey, s2cKey, err
		}
		if !time.Now().Before(deadline) {
			return w, c2sKey, s2cKey, fmt.Errorf("net: udp handshake with %s timed out", conn.RemoteAddr())
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
	plainBuf := make([]byte, 0, 65535)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			// The client's socket is connected, so an ICMP port unreachable, for example while the server restarts, comes back as ECONNREFUSED on the next read. It concerns one datagram: keep reading and let keepAlive's idle timeout decide whether the server is gone.
			if udpReadRetry(err) {
				continue
			}
			c.lost(conn, fmt.Errorf("%w: %w", ErrDisconnected, err))
			return
		}
		b := buf[:n]
		hdr, ok := parseSealed(b)
		if !ok || (hdr.typ != udpServerData && hdr.typ != udpPong) {
			continue
		}
		c.mu.Lock()
		if c.conn != conn || c.state != ConnOpen || hdr.peer != c.localID || !c.recv.wouldAccept(hdr.seq) {
			c.mu.Unlock()
			continue
		}
		pt, valid := c.recvCipher.open(plainBuf, udpDirServerToClient, b)
		if !valid {
			c.mu.Unlock()
			continue
		}
		c.recv.accept(hdr.seq)
		c.lastRecv = time.Now()
		if hdr.typ == udpServerData && hdr.ch == ChannelUnreliable {
			c.inbox.push(Packet{
				From:    c.serverID,
				Channel: hdr.ch,
				Payload: slices.Clone(pt),
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
				if seq, ok := nextSeq(&c.sendSeq); ok {
					c.lastSend = now
					ping = c.sendCipher.seal(nil, udpDirClientToServer, udpPing, c.localID, seq, ChannelUnreliable, nil)
				}
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

// Send encrypts and authenticates payload with the client-to-server ChaCha20-Poly1305 key and sends it to the server in one datagram. It refuses ChannelReliableOrdered with ErrUnsupportedChannel, returns ErrDisconnected while the connection is down, and returns ErrSequenceExhausted once the 32-bit sequence counter would wrap (call Reconnect to rekey).
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
	seq, ok := nextSeq(&c.sendSeq)
	if !ok {
		c.mu.Unlock()
		return ErrSequenceExhausted
	}
	c.lastSend = time.Now()
	dgram := c.sendCipher.seal(nil, udpDirClientToServer, udpData, c.localID, seq, ch, payload)
	conn := c.conn
	c.mu.Unlock()
	_, err := conn.Write(dgram)
	return err
}

// SetInboxLimits bounds the packets the client keeps until Recv takes them (see InboxLimits).
func (c *UDPClient) SetInboxLimits(l InboxLimits) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inbox.setLimits(l)
}

// InboxStats reports what the client's inbox holds and what its limits dropped or warned about.
func (c *UDPClient) InboxStats() InboxStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inbox.stats()
}

// Recv pops the next authenticated and decrypted packet from the server without blocking.
func (c *UDPClient) Recv() (Packet, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inbox.pop()
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

// Close sends the server an authenticated goodbye, so it reports PeerLeft at once rather than holding the session for a reconnect, and closes the socket.
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
		if seq, ok := nextSeq(&c.sendSeq); ok {
			bye = c.sendCipher.seal(nil, udpDirClientToServer, udpBye, c.localID, seq, ChannelUnreliable, nil)
		}
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
