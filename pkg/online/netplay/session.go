package netplay

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"slices"
	"time"
	"unicode/utf8"
)

// Auth configures the connection handshake that ListenWebSocket and ListenUDPServer run before a peer may send or receive game packets.
//
// The zero value accepts every client, assigns its ID and treats a dropped connection as a leave. A server reachable from the internet should set Verify: without it anyone may join, up to MaxPeers.
type Auth struct {
	// Verify checks the credential a client presented and the ID the server is about to assign it. A non-nil error rejects the client: it never reaches Recv, Peers or NextEvent, and the error's text is sent back as the reason. Nil accepts every client.
	//
	// The WebSocket server calls Verify on the client's own goroutine. The UDP server calls it on its receive goroutine, so a slow Verify delays every UDP peer's packets; check a signed token locally rather than calling a remote service there.
	Verify func(peer PeerID, token []byte) error
	// GracePeriod is how long a dropped peer keeps its ID and session. A client that reconnects within it resumes with the same ID and NextEvent reports PeerResumed; once it passes, NextEvent reports PeerLeft. Zero reports PeerLeft as soon as the connection drops.
	GracePeriod time.Duration
	// IdleTimeout is how long a peer may stay silent before the server counts it as dropped; 10 seconds when zero, and a value below 3 seconds is raised to 3 seconds. UDP clients ping every second while idle, so a gap of up to 2 seconds is normal. A WebSocket server pings each client every third of IdleTimeout and drops one that sends no frame, not even the pong, for IdleTimeout, so a client whose network vanished without closing the TCP connection does not keep its session forever. The WebSocket timeout runs on the wall clock, not on Now.
	IdleTimeout time.Duration
	// MaxPeers is the most sessions the server holds at once, counting peers inside their grace period and handshakes waiting on Verify; 1024 when zero. A client past it is rejected with the reason "server full", so a flood of handshakes cannot grow the server without bound.
	MaxPeers int
	// Now is the clock for grace periods and idle timeouts; nil uses time.Now.
	Now func() time.Time
	// StaticKey is the server's static X25519 private key for ListenUDPServer's Noise NK handshake. When nil, ListenUDPServer generates a fresh X25519 key pair (available via UDPServer.PublicKey); set a persistent key when clients know it from configuration rather than a matchmaking ticket, or must reconnect across server restarts. A non-X25519 key makes ListenUDPServer return ErrInvalidKey. Ignored by WebSocket servers, which use TLS.
	StaticKey *ecdh.PrivateKey
}

// PeerEventKind is what happened to a peer's session.
type PeerEventKind uint8

const (
	// PeerJoined reports a client that passed the handshake and received a new ID.
	PeerJoined PeerEventKind = iota + 1
	// PeerDropped reports a connection that broke without a goodbye; the peer keeps its ID for Auth.GracePeriod.
	PeerDropped
	// PeerResumed reports a dropped peer that reconnected within the grace period with its old ID.
	PeerResumed
	// PeerLeft reports a peer that closed its connection, or whose grace period ran out. Its ID is gone.
	PeerLeft
)

// String returns the event kind's name.
func (k PeerEventKind) String() string {
	switch k {
	case PeerJoined:
		return "joined"
	case PeerDropped:
		return "dropped"
	case PeerResumed:
		return "resumed"
	case PeerLeft:
		return "left"
	default:
		return "unknown"
	}
}

// PeerEvent is one change to a peer's session, reported by a server's NextEvent.
type PeerEvent struct {
	Kind PeerEventKind
	Peer PeerID
	// Reason is why a server removed the peer with WebSocketServer.Kick or UDPServer.Kick; it is empty for every other event.
	Reason string
}

// PeerEvents is implemented by the server transports that run the connection handshake. NextEvent pops the next session change without blocking; grace periods and idle timeouts are checked when it is called, so a server polls it every tick alongside Recv.
type PeerEvents interface {
	NextEvent() (PeerEvent, bool)
}

// ConnState is a client transport's connection state.
type ConnState uint8

const (
	// ConnConnecting is a client whose handshake has not finished.
	ConnConnecting ConnState = iota
	// ConnOpen is a client the server accepted.
	ConnOpen
	// ConnDisconnected is a client whose connection dropped or was rejected; Reconnect may resume it.
	ConnDisconnected
	// ConnClosed is a client closed with Close.
	ConnClosed
)

// String returns the state's name.
func (s ConnState) String() string {
	switch s {
	case ConnConnecting:
		return "connecting"
	case ConnOpen:
		return "open"
	case ConnDisconnected:
		return "disconnected"
	case ConnClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// MaxTokenSize is the largest credential a client may present in the handshake.
const MaxTokenSize = 4096

var (
	// ErrRejected matches (with errors.Is) the *RejectError a client gets when the server refuses its handshake.
	ErrRejected = errors.New("net: connection rejected")
	// ErrDisconnected is returned when sending on a client whose connection dropped, or to a server peer inside its grace period.
	ErrDisconnected = errors.New("net: peer disconnected")
	// ErrTokenTooLarge is returned when a token exceeds MaxTokenSize.
	ErrTokenTooLarge = errors.New("net: token too large")
	// ErrInvalidKey is returned when a Noise X25519 key is missing or uses another curve, or when AddPeerKey receives an all-zero key.
	ErrInvalidKey = errors.New("net: invalid key")
	// ErrSequenceExhausted is returned when a UDP session or keyed peer reaches the end of its 32-bit sequence space and cannot send without reusing an AEAD nonce. Call Reconnect (or re-register the peer with a new key) to rekey.
	ErrSequenceExhausted = errors.New("net: session sequence exhausted")
	// ErrKicked matches (with errors.Is) the *KickError a client gets when the server ends its session with WebSocketServer.Kick or UDPServer.Kick.
	ErrKicked = errors.New("net: kicked by server")
)

// RejectError is the reason a server gave for refusing a client's handshake.
type RejectError struct {
	Reason string
}

// Error returns the rejection with its reason.
func (e *RejectError) Error() string {
	return "net: connection rejected: " + e.Reason
}

// Is reports whether target is ErrRejected.
func (e *RejectError) Is(target error) bool {
	return target == ErrRejected
}

// KickError is the reason a server gave for removing a client's session with WebSocketServer.Kick or UDPServer.Kick.
type KickError struct {
	Reason string
}

// Error returns the kick with its reason.
func (e *KickError) Error() string {
	return "net: kicked by server: " + e.Reason
}

// Is reports whether target is ErrKicked.
func (e *KickError) Is(target error) bool {
	return target == ErrKicked
}

// truncateReason caps a kick or rejection reason at limit bytes, cutting on a rune boundary so a truncated reason stays valid UTF-8.
func truncateReason(reason string, limit int) string {
	if len(reason) <= limit {
		return reason
	}
	reason = reason[:limit]
	for !utf8.ValidString(reason) {
		reason = reason[:len(reason)-1]
	}
	return reason
}

// Handshake wire format, version 1. Every message starts with its type and the version, and the server echoes the client's nonce so a client can match a reply to its hello.
//
//	hello:   type(1) version(1) nonce(16) resumePeer(4) resumeSecret(16) tokenLen(2) token padding
//	welcome: type(1) version(1) nonce(16) peer(4) server(4) resumed(1) secret(16) key(32)
//	reject:  type(1) version(1) nonce(16) reasonLen(2) reason
const (
	hsVersion    = 1
	hsHello      = 0xA1
	hsWelcome    = 0xA2
	hsReject     = 0xA3
	hsNonceSize  = 16
	hsSecretSize = 16
	hsKeySize    = 32
	hsMaxReason  = 256

	// udpMinHello is the smallest hello a UDP server answers. A reply is never longer than the hello that caused it, so the server cannot amplify a spoofed request.
	udpMinHello = 128

	// udpMinIdle is the shortest Auth.IdleTimeout a UDP server uses: a client pings once a second, checked on a one-second ticker, so a healthy idle client can go up to 2 seconds between datagrams.
	udpMinIdle = 3 * time.Second

	// defaultMaxPeers is Auth.MaxPeers when zero.
	defaultMaxPeers = 1024
	// reasonServerFull is the rejection a client gets once the server holds Auth.MaxPeers sessions.
	reasonServerFull = "server full"

	// wsCloseReconnect is the WebSocket close code (from the range RFC 6455 leaves to applications) a client sends when it closes a socket to reconnect. The server counts it as a drop that keeps the session for the grace period, not as a goodbye.
	wsCloseReconnect = 4000
	// wsCloseKicked is the WebSocket close code a WebSocketServer sends when Kick removes a client, followed by the reason as the close frame's text.
	wsCloseKicked = 4001

	hsHelloHeader   = 2 + hsNonceSize + 4 + hsSecretSize + 2
	hsWelcomeSize   = 2 + hsNonceSize + 4 + 4 + 1 + hsSecretSize + hsKeySize
	hsRejectHeader  = 2 + hsNonceSize + 2
	hsReasonVersion = "unsupported handshake version"
)

var errHandshakeVersion = errors.New("net: unsupported handshake version")

type hello struct {
	nonce        [hsNonceSize]byte
	resumePeer   PeerID
	resumeSecret [hsSecretSize]byte
	token        []byte
}

type welcome struct {
	nonce   [hsNonceSize]byte
	peer    PeerID
	server  PeerID
	resumed bool
	secret  [hsSecretSize]byte
	key     [hsKeySize]byte
}

// encodeHello writes h, zero-padded to at least minSize bytes. UDP pads its hello so no reply is larger than the request that caused it.
func encodeHello(h hello, minSize int) []byte {
	n := max(hsHelloHeader+len(h.token), minSize)
	b := make([]byte, n)
	b[0], b[1] = hsHello, hsVersion
	copy(b[2:18], h.nonce[:])
	binary.BigEndian.PutUint32(b[18:22], uint32(h.resumePeer))
	copy(b[22:38], h.resumeSecret[:])
	binary.BigEndian.PutUint16(b[38:40], uint16(len(h.token)))
	copy(b[40:], h.token)
	return b
}

// decodeHello parses a hello. A hello of another version returns its nonce with errHandshakeVersion so the server can say why it refused.
func decodeHello(b []byte) (hello, error) {
	var h hello
	if len(b) < 2 || b[0] != hsHello {
		return h, ErrInvalidPacket
	}
	if len(b) >= 2+hsNonceSize {
		copy(h.nonce[:], b[2:18])
	}
	if b[1] != hsVersion {
		return h, errHandshakeVersion
	}
	if len(b) < hsHelloHeader {
		return h, ErrInvalidPacket
	}
	h.resumePeer = PeerID(binary.BigEndian.Uint32(b[18:22]))
	copy(h.resumeSecret[:], b[22:38])
	n := int(binary.BigEndian.Uint16(b[38:40]))
	if n > MaxTokenSize || hsHelloHeader+n > len(b) {
		return h, ErrInvalidPacket
	}
	h.token = slices.Clone(b[hsHelloHeader : hsHelloHeader+n])
	return h, nil
}

func encodeWelcome(w welcome) []byte {
	b := make([]byte, hsWelcomeSize)
	b[0], b[1] = hsWelcome, hsVersion
	copy(b[2:18], w.nonce[:])
	binary.BigEndian.PutUint32(b[18:22], uint32(w.peer))
	binary.BigEndian.PutUint32(b[22:26], uint32(w.server))
	if w.resumed {
		b[26] = 1
	}
	copy(b[27:43], w.secret[:])
	copy(b[43:75], w.key[:])
	return b
}

// encodeReject writes a rejection no longer than limit bytes (when limit > 0), truncating the reason to fit.
func encodeReject(nonce [hsNonceSize]byte, reason string, limit int) []byte {
	if len(reason) > hsMaxReason {
		reason = reason[:hsMaxReason]
	}
	if limit > 0 && hsRejectHeader+len(reason) > limit {
		reason = reason[:max(0, limit-hsRejectHeader)]
	}
	b := make([]byte, hsRejectHeader+len(reason))
	b[0], b[1] = hsReject, hsVersion
	copy(b[2:18], nonce[:])
	binary.BigEndian.PutUint16(b[18:20], uint16(len(reason)))
	copy(b[20:], reason)
	return b
}

// decodeServerHandshake parses a welcome or a rejection. A rejection returns a *RejectError.
func decodeServerHandshake(b []byte) (welcome, error) {
	var w welcome
	if len(b) < 2+hsNonceSize || b[1] != hsVersion {
		return w, ErrInvalidPacket
	}
	copy(w.nonce[:], b[2:18])
	switch b[0] {
	case hsWelcome:
		if len(b) != hsWelcomeSize || b[26] > 1 {
			return w, ErrInvalidPacket
		}
		w.peer = PeerID(binary.BigEndian.Uint32(b[18:22]))
		w.server = PeerID(binary.BigEndian.Uint32(b[22:26]))
		if w.peer == 0 || w.server == 0 || w.peer == w.server {
			return w, ErrInvalidPacket
		}
		w.resumed = b[26] == 1
		copy(w.secret[:], b[27:43])
		copy(w.key[:], b[43:75])
		return w, nil
	case hsReject:
		if len(b) < hsRejectHeader {
			return w, ErrInvalidPacket
		}
		n := int(binary.BigEndian.Uint16(b[18:20]))
		if n > hsMaxReason || hsRejectHeader+n != len(b) {
			return w, ErrInvalidPacket
		}
		return w, &RejectError{Reason: string(b[20:])}
	default:
		return w, ErrInvalidPacket
	}
}

// acceptServerHandshake parses the server's reply to the hello with nonce. A welcome for another hello is ErrInvalidPacket and a rejection is a *RejectError.
func acceptServerHandshake(b []byte, nonce [hsNonceSize]byte) (welcome, error) {
	w, err := decodeServerHandshake(b)
	if err == nil && w.nonce != nonce {
		return welcome{}, ErrInvalidPacket
	}
	return w, err
}

// newHello builds a client hello for token, resuming the session in res when it has one.
func newHello(token []byte, res resumeState) (hello, error) {
	if len(token) > MaxTokenSize {
		return hello{}, ErrTokenTooLarge
	}
	h := hello{token: token, resumePeer: res.peer, resumeSecret: res.secret}
	_, _ = rand.Read(h.nonce[:]) // crypto/rand.Read never fails since Go 1.24
	return h, nil
}

// resumeState is what a client keeps to resume its session after a drop.
type resumeState struct {
	peer   PeerID
	secret [hsSecretSize]byte
}

// session is the server's record of one authenticated peer, connected or inside its grace period.
type session struct {
	id        PeerID
	pending   bool // reserved while Verify runs
	connected bool
	secret    [hsSecretSize]byte
	droppedAt time.Time
	lastSeen  time.Time

	// The transport's link to the peer: the WebSocket connection, or the UDP address with its handshake reply, AEAD ciphers and packet counters.
	link any
}

// sessionTable assigns IDs and tracks sessions for one server transport. Its methods expect the transport's mutex to be held, except that Verify runs between reserve and commit without it.
type sessionTable struct {
	auth     Auth
	serverID PeerID
	next     PeerID
	idle     time.Duration // UDP only; zero disables idle drops
	maxPeers int
	sessions map[PeerID]*session
	events   fifo[PeerEvent]
}

func newSessionTable(auth Auth, serverID PeerID, udp bool) *sessionTable {
	t := &sessionTable{auth: auth, serverID: serverID, next: serverID + 1, maxPeers: auth.MaxPeers, sessions: make(map[PeerID]*session)}
	if t.maxPeers <= 0 {
		t.maxPeers = defaultMaxPeers
	}
	if udp {
		t.idle = auth.IdleTimeout
		if t.idle <= 0 {
			t.idle = 10 * time.Second
		}
		t.idle = max(t.idle, udpMinIdle)
	}
	return t
}

func (t *sessionTable) now() time.Time {
	if t.auth.Now != nil {
		return t.auth.Now()
	}
	return time.Now()
}

// rotate gives s a fresh resume secret, so a resume token seen on an old connection is useless afterwards.
func (s *session) rotate() {
	_, _ = rand.Read(s.secret[:])
}

// resume returns the live or dropped session that h proves it owns, marked connected with fresh secrets, or nil.
func (t *sessionTable) resume(h hello, now time.Time) *session {
	if h.resumePeer == 0 {
		return nil
	}
	s := t.sessions[h.resumePeer]
	if s == nil || s.pending || subtle.ConstantTimeCompare(s.secret[:], h.resumeSecret[:]) != 1 {
		return nil
	}
	if !s.connected {
		t.events.push(PeerEvent{Kind: PeerResumed, Peer: s.id})
	}
	s.connected = true
	s.droppedAt = time.Time{}
	s.lastSeen = now
	s.rotate()
	return s
}

// reserve allocates an unused ID and holds it while Verify runs. It returns false once the table holds maxPeers sessions.
func (t *sessionTable) reserve() (PeerID, bool) {
	if len(t.sessions) >= t.maxPeers {
		return 0, false
	}
	for t.next == 0 || t.next == t.serverID || t.sessions[t.next] != nil {
		t.next++
	}
	id := t.next
	t.next++
	t.sessions[id] = &session{id: id, pending: true}
	return id, true
}

// commit turns a reserved ID into a connected session.
func (t *sessionTable) commit(id PeerID, now time.Time) *session {
	s := t.sessions[id]
	s.pending = false
	s.connected = true
	s.lastSeen = now
	s.rotate()
	t.events.push(PeerEvent{Kind: PeerJoined, Peer: id})
	return s
}

// release frees a reserved ID whose Verify failed.
func (t *sessionTable) release(id PeerID) {
	if s := t.sessions[id]; s != nil && s.pending {
		delete(t.sessions, id)
	}
}

// drop marks a connected session dropped at the given time; without a grace period it leaves at once.
func (t *sessionTable) drop(s *session, at time.Time) {
	if !s.connected {
		return
	}
	s.connected = false
	s.link = nil
	if t.auth.GracePeriod <= 0 {
		t.leave(s)
		return
	}
	s.droppedAt = at
	t.events.push(PeerEvent{Kind: PeerDropped, Peer: s.id})
}

// leave removes a session for good.
func (t *sessionTable) leave(s *session) {
	t.leaveReason(s, "")
}

// leaveReason removes a session for good, recording reason on the PeerLeft event when the removal is a kick.
func (t *sessionTable) leaveReason(s *session, reason string) {
	if t.sessions[s.id] != s {
		return
	}
	delete(t.sessions, s.id)
	s.connected = false
	s.link = nil
	t.events.push(PeerEvent{Kind: PeerLeft, Peer: s.id, Reason: reason})
}

// expire drops UDP sessions that went silent and removes dropped sessions whose grace period has passed, in ID order so events are deterministic.
func (t *sessionTable) expire(now time.Time) {
	ids := make([]PeerID, 0, len(t.sessions))
	for id := range t.sessions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		s := t.sessions[id]
		if s.pending {
			continue
		}
		if s.connected && t.idle > 0 && now.Sub(s.lastSeen) >= t.idle {
			t.drop(s, s.lastSeen.Add(t.idle))
		}
		if !s.connected && t.sessions[id] == s && now.Sub(s.droppedAt) >= t.auth.GracePeriod {
			t.leave(s)
		}
	}
}

// connectedIDs returns the sorted IDs of connected sessions.
func (t *sessionTable) connectedIDs() []PeerID {
	out := make([]PeerID, 0, len(t.sessions))
	for id, s := range t.sessions {
		if s.connected {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// sendable reports why a server cannot send to peer: nil for a connected one, ErrDisconnected inside its grace period, ErrUnknownPeer otherwise.
func (t *sessionTable) sendable(peer PeerID) error {
	s := t.sessions[peer]
	switch {
	case s == nil || s.pending:
		return ErrUnknownPeer
	case !s.connected:
		return ErrDisconnected
	default:
		return nil
	}
}

func (t *sessionTable) popEvent() (PeerEvent, bool) {
	return t.events.pop()
}

// welcomeFor builds the welcome that tells the client its session.
func (t *sessionTable) welcomeFor(s *session, nonce [hsNonceSize]byte, resumed bool) welcome {
	return welcome{nonce: nonce, peer: s.id, server: t.serverID, resumed: resumed, secret: s.secret}
}

// verify runs Auth.Verify and turns its error into a reason for the client.
func (t *sessionTable) verify(id PeerID, token []byte) (string, bool) {
	if t.auth.Verify == nil {
		return "", true
	}
	if err := t.auth.Verify(id, token); err != nil {
		reason := err.Error()
		if reason == "" {
			reason = "rejected"
		}
		return reason, false
	}
	return "", true
}
