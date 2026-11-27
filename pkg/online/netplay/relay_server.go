//go:build !js

package netplay

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

const (
	// relayResend is how often the relay sends each member its session's membership again, so a client whose update was lost on UDP learns it within a second, and how often it checks grace periods.
	relayResend = time.Second
	// relayPoll is how often Serve polls a server transport that cannot wake it.
	relayPoll = time.Millisecond

	defaultRelaySessionPeers = 8
	defaultRelayQueue        = 256

	reasonSessionFull = "session full"
	reasonNoSession   = "no session"
)

// RelayConfig configures a Relay.
type RelayConfig struct {
	// Join admits a client to a session: it checks the token the client presented in the connection handshake and returns the name of the session the client joins, or an error whose text is sent back as the rejection reason. Packets are forwarded only between members of the same session. Join is required. It runs where Auth.Verify runs: on the client's goroutine for WebSocket and on the receive goroutine for UDP, so check a signed token locally.
	Join func(peer PeerID, token []byte) (session string, err error)
	// MaxSessionPeers is the most members a session holds, counting members inside their grace period; 8 when zero. A client past it is rejected with the reason "session full".
	MaxSessionPeers int
	// QueueSize is how many frames the relay holds for one member that has not been written yet; 256 when zero. Frames past it are dropped, and the senders' RelayTransports resend the reliable ones.
	QueueSize int
}

// RelayStats counts a relay's sessions and traffic.
type RelayStats struct {
	// Sessions is the number of sessions with at least one member.
	Sessions int
	// Peers is the number of members across all sessions, including members inside their grace period.
	Peers int
	// Forwarded counts frames queued for a member, including the relay's own membership updates.
	Forwarded uint64
	// Dropped counts frames refused: malformed, from a client in no session, addressed outside the sender's session, or past a member's queue.
	Dropped uint64
}

// RelayServer is a server transport a Relay serves: WebSocketServer or UDPServer.
type RelayServer interface {
	Transport
	PeerEvents
}

// relayWaker is implemented by the server transports that can wake Serve when a packet or event arrives, so the relay forwards without polling.
type relayWaker interface {
	relayWake() <-chan struct{}
}

// Relay forwards packets between the peers of a peer-to-peer session, so lockstep and rollback work for players who cannot reach each other directly: each player connects to the relay with a RelayTransport over WebSocket or UDP, and the relay passes each frame to the member it is addressed to.
//
// The relay sits on a WebSocketServer or UDPServer, so every client passes that server's connection handshake first; Join then decides which session the client belongs to. A frame addressed outside the sender's session is dropped. The relay never reads the payloads, and reliability is end to end between RelayTransports, so the relay may drop a frame for a member that is reconnecting or too slow without breaking ChannelReliableOrdered.
type Relay struct {
	join     func(PeerID, []byte) (string, error)
	maxPeers int
	queue    int

	mu        sync.Mutex
	srv       RelayServer // the server Serve was first called with
	serving   bool
	pending   map[PeerID]string // verified, not yet joined
	sessionOf map[PeerID]string
	sessions  map[string]*relaySession
	writers   map[PeerID]*relayWriter
	forwarded uint64
	dropped   uint64
	writersWG sync.WaitGroup
}

type relaySession struct {
	members []PeerID // sorted
	// epoch counts membership changes and wraps; resending an unchanged membership keeps it.
	epoch uint32
	dirty bool
}

// relayWriter sends one member's frames on its own goroutine, so a member whose connection is slow delays only itself.
type relayWriter struct {
	q chan []byte
}

// NewRelay returns a relay for cfg. It fails when cfg.Join is nil.
func NewRelay(cfg RelayConfig) (*Relay, error) {
	if cfg.Join == nil {
		return nil, errors.New("net: relay needs a Join function")
	}
	r := &Relay{
		join:      cfg.Join,
		maxPeers:  cfg.MaxSessionPeers,
		queue:     cfg.QueueSize,
		pending:   make(map[PeerID]string),
		sessionOf: make(map[PeerID]string),
		sessions:  make(map[string]*relaySession),
		writers:   make(map[PeerID]*relayWriter),
	}
	if r.maxPeers <= 0 {
		r.maxPeers = defaultRelaySessionPeers
	}
	if r.queue <= 0 {
		r.queue = defaultRelayQueue
	}
	return r, nil
}

// Auth returns base with Verify replaced by the relay's admission: base.Verify, when set, checks the token first, then Join picks the session and the relay refuses a full one. Pass the result to ListenWebSocket, ListenWebSocketTLS or ListenUDPServer, and serve that server with Serve.
func (r *Relay) Auth(base Auth) Auth {
	verify := base.Verify
	base.Verify = func(peer PeerID, token []byte) error {
		if verify != nil {
			if err := verify(peer, token); err != nil {
				return err
			}
		}
		return r.admit(peer, token)
	}
	return base
}

// admit runs Join and reserves a place in the chosen session.
func (r *Relay) admit(peer PeerID, token []byte) error {
	name, err := r.join(peer, token)
	if err != nil {
		return err
	}
	if name == "" {
		return errors.New(reasonNoSession)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	if s := r.sessions[name]; s != nil {
		n = len(s.members)
	}
	for _, other := range r.pending {
		if other == name {
			n++
		}
	}
	if n >= r.maxPeers {
		return errors.New(reasonSessionFull)
	}
	r.pending[peer] = name
	return nil
}

// Stats returns the relay's current counts.
func (r *Relay) Stats() RelayStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return RelayStats{Sessions: len(r.sessions), Peers: len(r.sessionOf), Forwarded: r.forwarded, Dropped: r.dropped}
}

// Serve forwards srv's packets until ctx ends, then stops the members' writers and returns ctx's error. srv must have been created with r.Auth. A relay serves one server for its lifetime: Serve may be called again on the same server after it returns, and the clients still connected keep their sessions, but a concurrent Serve or a Serve on another server returns an error. Serve does not close srv.
func (r *Relay) Serve(ctx context.Context, srv RelayServer) error {
	r.mu.Lock()
	switch {
	case r.serving:
		r.mu.Unlock()
		return errors.New("net: relay is already serving")
	case r.srv != nil && r.srv != srv:
		r.mu.Unlock()
		return errors.New("net: relay serves another server")
	}
	r.srv = srv
	r.serving = true
	for id := range r.sessionOf {
		r.startWriterLocked(srv, id)
	}
	for _, s := range r.sessions {
		s.dirty = true
	}
	r.mu.Unlock()
	defer r.stop()

	var wake <-chan struct{}
	var poll <-chan time.Time
	if w, ok := srv.(relayWaker); ok {
		wake = w.relayWake()
	} else {
		ticker := time.NewTicker(relayPoll)
		defer ticker.Stop()
		poll = ticker.C
	}
	resend := time.NewTicker(relayResend)
	defer resend.Stop()

	for {
		r.drainEvents(srv)
		r.drainPackets(srv)
		r.flushMembers()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
		case <-poll:
		case <-resend.C:
			r.markAllDirty()
		}
	}
}

// stop closes every writer and waits for them. Sessions are kept, so a later Serve on the same server goes on forwarding for the clients still connected.
func (r *Relay) stop() {
	r.mu.Lock()
	for id, w := range r.writers {
		close(w.q)
		delete(r.writers, id)
	}
	r.serving = false
	r.mu.Unlock()
	r.writersWG.Wait()
}

func (r *Relay) drainEvents(srv RelayServer) {
	for {
		ev, ok := srv.NextEvent()
		if !ok {
			return
		}
		r.mu.Lock()
		switch ev.Kind {
		case PeerJoined:
			r.joinedLocked(srv, ev.Peer)
		case PeerResumed:
			if s := r.sessions[r.sessionOf[ev.Peer]]; s != nil {
				s.dirty = true
			}
		case PeerLeft:
			r.leftLocked(ev.Peer)
		}
		r.mu.Unlock()
	}
}

// joinedLocked moves a verified peer into its session and starts its writer. A peer that joined without the relay's Auth belongs to no session, and its frames are dropped.
func (r *Relay) joinedLocked(srv RelayServer, id PeerID) {
	name, ok := r.pending[id]
	if !ok {
		return
	}
	delete(r.pending, id)
	s := r.sessions[name]
	if s == nil {
		s = &relaySession{}
		r.sessions[name] = s
	}
	if i, found := slices.BinarySearch(s.members, id); !found {
		s.members = slices.Insert(s.members, i, id)
		s.epoch++
	}
	s.dirty = true
	r.sessionOf[id] = name
	r.startWriterLocked(srv, id)
}

// startWriterLocked starts the goroutine that sends a member's queued frames.
func (r *Relay) startWriterLocked(srv RelayServer, id PeerID) {
	w := &relayWriter{q: make(chan []byte, r.queue)}
	r.writers[id] = w
	r.writersWG.Add(1)
	go func() {
		defer r.writersWG.Done()
		for frame := range w.q {
			_ = srv.Send(id, ChannelUnreliable, frame)
		}
	}()
}

func (r *Relay) leftLocked(id PeerID) {
	delete(r.pending, id)
	name, ok := r.sessionOf[id]
	if !ok {
		return
	}
	delete(r.sessionOf, id)
	if w := r.writers[id]; w != nil {
		close(w.q)
		delete(r.writers, id)
	}
	s := r.sessions[name]
	if i, found := slices.BinarySearch(s.members, id); found {
		s.members = slices.Delete(s.members, i, i+1)
		s.epoch++
	}
	if len(s.members) == 0 {
		delete(r.sessions, name)
		return
	}
	s.dirty = true
}

func (r *Relay) drainPackets(srv RelayServer) {
	for {
		pkt, ok := srv.Recv()
		if !ok {
			return
		}
		r.mu.Lock()
		_, known := r.sessionOf[pkt.From]
		r.mu.Unlock()
		if !known {
			// The join event may have been queued after this loop last drained events.
			r.drainEvents(srv)
		}
		r.mu.Lock()
		r.routeLocked(pkt)
		r.mu.Unlock()
	}
}

// routeLocked forwards one client frame to the member it names, if both are in the same session.
func (r *Relay) routeLocked(pkt Packet) {
	to, body, ok := decodeRelayFrame(pkt.Payload)
	name, member := r.sessionOf[pkt.From]
	if !ok || !member || to == 0 || to == pkt.From || r.sessionOf[to] != name {
		r.dropped++
		return
	}
	r.enqueueLocked(to, encodeRelayFrame(pkt.From, body))
}

func (r *Relay) enqueueLocked(to PeerID, frame []byte) {
	w := r.writers[to]
	if w == nil {
		r.dropped++
		return
	}
	select {
	case w.q <- frame:
		r.forwarded++
	default:
		r.dropped++
	}
}

func (r *Relay) markAllDirty() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		s.dirty = true
	}
}

// flushMembers sends each dirty session's membership to its members, in session-name order so output is deterministic.
func (r *Relay) flushMembers() {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.sessions))
	for name, s := range r.sessions {
		if s.dirty {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		s := r.sessions[name]
		s.dirty = false
		frame := encodeRelayMembers(s.epoch, s.members)
		for _, id := range s.members {
			r.enqueueLocked(id, frame)
		}
	}
}
