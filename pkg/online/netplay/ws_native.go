//go:build !js

package netplay

import (
	"bufio"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	stdnet "net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nsgo-dev/game/pkg/online/internal/wsproto"
)

const (
	maxWSHandshakeSize = 1 << 20 // 1 MiB cap on the HTTP upgrade request's headers
	wsWriteTimeout     = 10 * time.Second
	// wsCloseFlushTimeout bounds how long WebSocketServer.Kick and Close let a client's writer flush the frames already queued for it, and the close frame after them, before they cut the connection.
	wsCloseFlushTimeout = time.Second
	wsDialTimeout       = 10 * time.Second
	wsHandshakeTimeout  = 10 * time.Second

	// wsMaxHelloFrame bounds the first frame a server reads from a client, which is the hello: its header and a token of at most MaxTokenSize bytes.
	wsMaxHelloFrame = hsHelloHeader + MaxTokenSize
	// wsMaxReplyFrame bounds the welcome or reject a client reads in the handshake.
	wsMaxReplyFrame = 1 << 10

	// wsMinIdle is the shortest Auth.IdleTimeout a WebSocket server uses, as for UDP.
	wsMinIdle = 3 * time.Second
	// wsClientPing is how often a native client pings the server, and wsClientIdle how long it waits for any frame before it counts the server as gone. Pinging much more often than the idle timeout keeps a healthy connection alive whatever interval the server pings at.
	wsClientPing = 2 * time.Second
	wsClientIdle = 10 * time.Second
)

// The RFC 6455 framing lives in wsproto, shared with pkg/online/wsclient; these aliases keep the netplay call sites, and the frame-level internal tests, unchanged.
const (
	wsOpContinuation    = wsproto.OpContinuation
	wsOpText            = wsproto.OpText
	wsOpBinary          = wsproto.OpBinary
	wsOpClose           = wsproto.OpClose
	wsOpPing            = wsproto.OpPing
	wsOpPong            = wsproto.OpPong
	wsMaxControlPayload = wsproto.MaxControlPayload
	maxWSMessageSize    = wsproto.MaxMessageSize
	wsEagerAlloc        = wsproto.EagerAlloc
)

var (
	wsAcceptKey      = wsproto.AcceptKey
	readWSFrame      = wsproto.ReadFrame
	readWSFrameLimit = wsproto.ReadFrameLimit
	writeWSFrame     = wsproto.WriteFrame
)

// writeWSFrameDeadline writes one frame with wsWriteTimeout as its deadline, unless closing is set. Close sets closing before it breaks pending writes with a deadline of now, and this checks closing after setting its own deadline, so a writer that takes the lock after Close began either sees closing or set its deadline before Close's: it can never push the deadline past Close's and hold Close up for wsWriteTimeout.
func writeWSFrameDeadline(conn stdnet.Conn, closing *atomic.Bool, opcode byte, masked bool, payload []byte) error {
	return writeWSFrameBy(conn, closing, time.Now().Add(wsWriteTimeout), opcode, masked, payload)
}

// writeWSFrameBy is writeWSFrameDeadline with deadline as the write deadline.
func writeWSFrameBy(conn stdnet.Conn, closing *atomic.Bool, deadline time.Time, opcode byte, masked bool, payload []byte) error {
	_ = conn.SetWriteDeadline(deadline)
	if closing.Load() {
		_ = conn.SetWriteDeadline(time.Now())
		return ErrClosed
	}
	err := writeWSFrame(conn, opcode, masked, payload)
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

// wsKeepAlive is how often a WebSocket endpoint pings its peer, and how long it waits for any frame, a pong included, before it counts the connection as dropped. Without it a peer whose network vanished without a FIN or RST, a half-open connection, would keep its session forever (#546).
type wsKeepAlive struct {
	ping time.Duration
	idle time.Duration
}

// serverKeepAlive derives the server's keepalive from Auth.IdleTimeout: 10 seconds when zero, at least wsMinIdle, with a ping every third of it so a client that answers pings is never idle that long.
func serverKeepAlive(auth Auth) wsKeepAlive {
	idle := auth.IdleTimeout
	if idle <= 0 {
		idle = 10 * time.Second
	}
	idle = max(idle, wsMinIdle)
	return wsKeepAlive{ping: idle / 3, idle: idle}
}

// clientKeepAlive is the native client's keepalive.
var clientKeepAlive = wsKeepAlive{ping: wsClientPing, idle: wsClientIdle}

// wsPingLoop pings the peer every interval until stop closes or closing is set. A ping that cannot be written closes the connection, so the read loop ends and reports the drop.
func wsPingLoop(conn stdnet.Conn, writeMu *sync.Mutex, closing *atomic.Bool, masked bool, every time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			writeMu.Lock()
			err := writeWSFrameDeadline(conn, closing, wsOpPing, masked, nil)
			writeMu.Unlock()
			if errors.Is(err, ErrClosed) {
				return // Close owns the connection now and sends the close frame itself
			}
			if err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}

// wsIdleReader reads from a connection and, once armed with an idle timeout, moves the read deadline to now plus that timeout before every read. A connection is dropped only when no byte at all arrives for the idle timeout, so a large frame that arrives slowly but steadily is read whole, while a stalled peer, or one whose network vanished, still times out (#546).
type wsIdleReader struct {
	conn stdnet.Conn
	idle time.Duration // zero until the handshake is done, so the handshake's own deadline stays in force
}

func (r *wsIdleReader) Read(p []byte) (int, error) {
	if r.idle > 0 {
		_ = r.conn.SetReadDeadline(time.Now().Add(r.idle))
	}
	return r.conn.Read(p)
}

// WebSocketTransport is a client-side RFC 6455 WebSocket Transport. Binary WebSocket frames carry the 5-byte channel+seq wire header.
//
// The first message each way is the connection handshake: the client presents its token and the server answers with the ID it assigned, or a reason for refusing. A dropped client keeps its session on the server for Auth.GracePeriod, and Reconnect resumes it with the same ID.
type WebSocketTransport struct {
	rawURL string
	token  []byte
	ka     wsKeepAlive

	dialMu  sync.Mutex // serializes Reconnect
	writeMu sync.Mutex
	closing atomic.Bool // set by Close, see writeWSFrameDeadline
	seq     [2]uint32

	mu       sync.Mutex
	conn     stdnet.Conn
	br       *bufio.Reader
	localID  PeerID
	serverID PeerID
	state    ConnState
	err      error
	resume   resumeState
	resumed  bool
	inbox    packetQueue
	changed  chan struct{}

	// admissions counts handshakes that admitted the client as a new peer rather than resuming, so a RelayTransport can tell its relay session was lost even when the new ID equals the old one.
	admissions uint64
}

// webSocketHostPort joins the URL's host and port, defaulting by scheme; see wsproto.HostPort.
func webSocketHostPort(u *url.URL) (string, error) {
	return wsproto.HostPort(u)
}

// DialWebSocket opens an RFC 6455 WebSocket connection to rawURL (ws:// or wss://), presents token in the connection handshake and returns a Transport ready for non-blocking Send/Recv once the server accepts it. The server assigns the client's ID, which LocalID reports. A refusal returns a *RejectError that matches ErrRejected.
//
// The client pings the server every 2 seconds and counts the connection as dropped, reporting ConnDisconnected, when no frame arrives for 10 seconds.
func DialWebSocket(rawURL string, token []byte) (*WebSocketTransport, error) {
	return dialWebSocket(rawURL, token, clientKeepAlive)
}

// DialWebSocketResume opens a client with a stored opaque resume token. If the
// server no longer retains the session, the handshake safely becomes a fresh join.
func DialWebSocketResume(rawURL string, token []byte, resume string) (*WebSocketTransport, error) {
	if len(token) > MaxTokenSize {
		return nil, ErrTokenTooLarge
	}
	r, err := decodeResumeToken(resume)
	if err != nil {
		return nil, err
	}
	wt := &WebSocketTransport{rawURL: rawURL, token: slices.Clone(token), ka: clientKeepAlive, resume: r, changed: make(chan struct{}, 1)}
	if err := wt.connect(); err != nil {
		return nil, err
	}
	return wt, nil
}

// ResumeToken returns the opaque credential for the current session.
func (wt *WebSocketTransport) ResumeToken() string {
	if wt == nil {
		return ""
	}
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if wt.resume.peer == 0 {
		return ""
	}
	return encodeResumeToken(wt.resume)
}

func dialWebSocket(rawURL string, token []byte, ka wsKeepAlive) (*WebSocketTransport, error) {
	if len(token) > MaxTokenSize {
		return nil, ErrTokenTooLarge
	}
	wt := &WebSocketTransport{
		rawURL:  rawURL,
		token:   slices.Clone(token),
		ka:      ka,
		changed: make(chan struct{}, 1),
	}
	if err := wt.connect(); err != nil {
		return nil, err
	}
	return wt, nil
}

// Reconnect drops the current connection, if any, without a goodbye, dials the server again and resumes the session: within the server's grace period the client keeps its ID, and Resumed reports true. Past it, the server admits the client as a new peer when its token is still valid, and LocalID changes.
func (wt *WebSocketTransport) Reconnect() error {
	wt.dialMu.Lock()
	defer wt.dialMu.Unlock()
	wt.mu.Lock()
	if wt.state == ConnClosed {
		wt.mu.Unlock()
		return ErrClosed
	}
	old := wt.conn
	wt.conn, wt.br = nil, nil
	wt.state = ConnConnecting
	wt.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return wt.connect()
}

// connect dials, upgrades and runs the handshake, then starts the read loop.
func (wt *WebSocketTransport) connect() error {
	wt.mu.Lock()
	if wt.state == ConnClosed {
		wt.mu.Unlock()
		return ErrClosed
	}
	res := wt.resume
	wt.mu.Unlock()

	conn, br, ir, err := dialWebSocketConn(wt.rawURL)
	if err != nil {
		return wt.fail(err)
	}
	w, err := clientHandshake(conn, br, wt.token, res)
	if err != nil {
		_ = conn.Close()
		return wt.fail(err)
	}
	_ = conn.SetDeadline(time.Time{})
	// Any byte from the server, its pongs and pings included, proves the connection is alive.
	ir.idle = wt.ka.idle

	wt.mu.Lock()
	if wt.state == ConnClosed {
		wt.mu.Unlock()
		_ = conn.Close()
		return ErrClosed
	}
	wt.conn, wt.br = conn, br
	wt.localID, wt.serverID = w.peer, w.server
	wt.resume = resumeState{peer: w.peer, secret: w.secret}
	wt.resumed = w.resumed
	if !w.resumed {
		wt.admissions++
	}
	wt.state = ConnOpen
	wt.err = nil
	wt.mu.Unlock()
	stop := make(chan struct{})
	go wsPingLoop(conn, &wt.writeMu, &wt.closing, true, wt.ka.ping, stop)
	go wt.readLoop(conn, br, stop)
	return nil
}

// fail records a failed connect.
func (wt *WebSocketTransport) fail(err error) error {
	wt.mu.Lock()
	if wt.state != ConnClosed {
		wt.state = ConnDisconnected
		wt.err = err
	}
	wt.mu.Unlock()
	return err
}

// clientHandshake sends the hello and reads the server's answer within the dial's deadline.
func clientHandshake(conn stdnet.Conn, br *bufio.Reader, token []byte, res resumeState) (welcome, error) {
	h, err := newHello(token, res)
	if err != nil {
		return welcome{}, err
	}
	if err := writeWSFrame(conn, wsOpBinary, true, encodeHello(h, 0)); err != nil {
		return welcome{}, err
	}
	fin, op, payload, _, err := readWSFrameLimit(br, wsMaxReplyFrame)
	if err != nil {
		return welcome{}, fmt.Errorf("net: websocket handshake read: %w", err)
	}
	if !fin || op != wsOpBinary {
		return welcome{}, ErrInvalidPacket
	}
	return acceptServerHandshake(payload, h.nonce)
}

// dialWebSocketConn opens the TCP (or TLS) connection and performs the HTTP upgrade. The returned connection still has the handshake deadline set, and the returned wsIdleReader, under the bufio.Reader, is not armed yet.
func dialWebSocketConn(rawURL string) (stdnet.Conn, *bufio.Reader, *wsIdleReader, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("net: parse websocket url: %w", err)
	}
	host, err := webSocketHostPort(u)
	if err != nil {
		return nil, nil, nil, err
	}

	dialer := &stdnet.Dialer{Timeout: wsDialTimeout}
	var conn stdnet.Conn
	if u.Scheme == "wss" {
		conn, err = tls.DialWithDialer(dialer, "tcp", host, &tls.Config{ServerName: u.Hostname()})
	} else {
		conn, err = dialer.Dial("tcp", host)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(wsHandshakeTimeout))

	ir := &wsIdleReader{conn: conn}
	br := bufio.NewReader(ir)
	if _, err := wsproto.ClientUpgrade(conn, br, u, nil, nil); err != nil {
		_ = conn.Close()
		return nil, nil, nil, err
	}
	return conn, br, ir, nil
}

func (wt *WebSocketTransport) readLoop(conn stdnet.Conn, br *bufio.Reader, stop chan struct{}) {
	cause := ErrDisconnected
	defer func() {
		close(stop)
		wt.mu.Lock()
		if wt.conn == conn {
			wt.conn, wt.br = nil, nil
			if wt.state == ConnOpen {
				wt.state = ConnDisconnected
				wt.err = cause
			}
		}
		wt.mu.Unlock()
		_ = conn.Close()
		wt.notify()
	}()

	var fragBuf []byte
	var fragOp byte
	inFrag := false

	for {
		fin, op, payload, _, err := readWSFrame(br)
		if err != nil {
			cause = fmt.Errorf("%w: %w", ErrDisconnected, err)
			return
		}
		switch op {
		case wsOpPing:
			wt.writeMu.Lock()
			_ = writeWSFrameDeadline(conn, &wt.closing, wsOpPong, true, payload)
			wt.writeMu.Unlock()
		case wsOpPong:
			continue
		case wsOpClose:
			if wsCloseCode(payload) == wsCloseKicked {
				reason := ""
				if len(payload) > 2 {
					reason = string(payload[2:])
				}
				cause = &KickError{Reason: reason}
			}
			wt.writeMu.Lock()
			_ = writeWSFrameDeadline(conn, &wt.closing, wsOpClose, true, nil)
			wt.writeMu.Unlock()
			return
		case wsOpBinary, wsOpText:
			if !fin {
				fragOp = op
				fragBuf = append(fragBuf[:0], payload...)
				inFrag = true
				continue
			}
			if op == wsOpBinary {
				wt.deliverBinary(conn, payload)
			}
		case wsOpContinuation:
			if !inFrag {
				continue
			}
			if len(fragBuf)+len(payload) > maxWSMessageSize {
				return
			}
			fragBuf = append(fragBuf, payload...)
			if fin {
				if fragOp == wsOpBinary {
					cp := make([]byte, len(fragBuf))
					copy(cp, fragBuf)
					wt.deliverBinary(conn, cp)
				}
				fragBuf = fragBuf[:0]
				inFrag = false
			}
		}
	}
}

func (wt *WebSocketTransport) deliverBinary(conn stdnet.Conn, frame []byte) {
	ch, seq, payload, err := decodeWireFrame(frame)
	if err != nil {
		return
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	wt.mu.Lock()
	if wt.conn == conn && wt.state == ConnOpen {
		wt.inbox.push(Packet{
			From:    wt.serverID,
			Channel: ch,
			Payload: cp,
			Seq:     seq,
		})
	}
	wt.mu.Unlock()
	wt.notify()
}

// notify wakes a waiter on changed without blocking.
func (wt *WebSocketTransport) notify() {
	select {
	case wt.changed <- struct{}{}:
	default:
	}
}

// Send frames payload with the 5-byte channel+seq header and sends it as a masked RFC 6455 binary WebSocket frame to the server. It returns ErrDisconnected while the connection is down.
func (wt *WebSocketTransport) Send(to PeerID, ch Channel, payload []byte) error {
	wt.mu.Lock()
	state, conn, serverID := wt.state, wt.conn, wt.serverID
	wt.mu.Unlock()
	switch {
	case state == ConnClosed:
		return ErrClosed
	case state != ConnOpen || conn == nil:
		return ErrDisconnected
	}
	if to != PeerBroadcast && to != serverID {
		return ErrUnknownPeer
	}
	if ch != ChannelReliableOrdered && ch != ChannelUnreliable {
		return ErrInvalidPacket
	}

	wt.writeMu.Lock()
	defer wt.writeMu.Unlock()
	wt.seq[ch]++
	frame := encodeWireFrame(ch, wt.seq[ch], payload)
	return writeWSFrameDeadline(conn, &wt.closing, wsOpBinary, true, frame)
}

// SetInboxLimits bounds the packets the transport keeps until Recv takes them (see InboxLimits).
func (wt *WebSocketTransport) SetInboxLimits(l InboxLimits) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	wt.inbox.setLimits(l)
}

// InboxStats reports what the transport's inbox holds and what its limits dropped or warned about.
func (wt *WebSocketTransport) InboxStats() InboxStats {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.inbox.stats()
}

// Recv pops the next received packet without blocking.
func (wt *WebSocketTransport) Recv() (Packet, bool) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.inbox.pop()
}

// LocalID returns the PeerID the server assigned this client.
func (wt *WebSocketTransport) LocalID() PeerID {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.localID
}

// Peers returns the connected server's PeerID, or nil while disconnected or closed.
func (wt *WebSocketTransport) Peers() []PeerID {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if wt.state != ConnOpen {
		return nil
	}
	return []PeerID{wt.serverID}
}

// State returns the connection state.
func (wt *WebSocketTransport) State() ConnState {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.state
}

// Err returns why the client is disconnected: a *RejectError, or ErrDisconnected wrapping the I/O error. It is nil while the connection is open.
func (wt *WebSocketTransport) Err() error {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.err
}

// Resumed reports whether the last handshake resumed an existing session, keeping the client's ID.
func (wt *WebSocketTransport) Resumed() bool {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.resumed
}

// relayAdmissions counts the handshakes that admitted the client as a new peer.
func (wt *WebSocketTransport) relayAdmissions() uint64 {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.admissions
}

// Close sends a WebSocket close frame and closes the underlying connection. The server reports PeerLeft at once rather than holding the session for a reconnect.
func (wt *WebSocketTransport) Close() error {
	wt.mu.Lock()
	if wt.state == ConnClosed {
		wt.mu.Unlock()
		return nil
	}
	wt.state = ConnClosed
	conn := wt.conn
	wt.conn, wt.br = nil, nil
	wt.mu.Unlock()
	if conn == nil {
		return nil
	}

	// Break a Send or ping already blocked on a server that does not read, then send the close frame with its own short deadline.
	wt.closing.Store(true)
	_ = conn.SetWriteDeadline(time.Now())
	wt.writeMu.Lock()
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	_ = writeWSFrame(conn, wsOpClose, true, nil)
	wt.writeMu.Unlock()
	return conn.Close()
}

type wsClientConn struct {
	id      PeerID
	conn    stdnet.Conn
	br      *bufio.Reader
	writeMu sync.Mutex
	closing atomic.Bool // set before the close frame, when a Kick's or Close's flush runs out of time, and by an outbox overflow; see writeWSFrameDeadline
	out     *outbox     // the frames Send queued and writeClientLoop has not written yet
	// writerDone is closed when writeClientLoop returns.
	writerDone chan struct{}
	// writeTimeout bounds each write of a queued frame; admit copies it from the server.
	writeTimeout time.Duration
}

// shutdown ends the connection without waiting for it: the writer flushes the frames already queued and then writes a close frame carrying closePayload, and closes the connection. Whatever is not written by flushBy, because the client reads too slowly or not at all, is cut off then: the pending write breaks and the connection closes. shutdown never touches the connection on the caller's goroutine, since even closing a TLS connection writes an alert to a client that may not be reading.
func (c *wsClientConn) shutdown(closePayload []byte, flushBy time.Time) {
	if !c.out.finish(closePayload, flushBy) {
		// The writer has already stopped because the connection ended: only closing it is left.
		c.closing.Store(true)
		_ = c.conn.SetWriteDeadline(time.Now())
		go func() { _ = c.conn.Close() }()
		return
	}
	time.AfterFunc(time.Until(flushBy), func() {
		select {
		case <-c.writerDone:
			return
		default:
		}
		// Break the queued frame, close frame, ping or welcome still being written, then close.
		c.closing.Store(true)
		_ = c.conn.SetWriteDeadline(time.Now())
		_ = c.conn.Close()
	})
}

// WebSocketServer accepts RFC 6455 WebSocket clients and exposes them through the standard Transport interface.
//
// Every client runs the connection handshake before its packets reach Recv: Auth.Verify checks its token, the server assigns its ID and tells the client, and NextEvent reports joins, drops, resumes and leaves.
//
// The server pings every client and reports PeerDropped for one that stays silent for Auth.IdleTimeout. Browsers answer pings on their own, so browser clients need no code for it.
//
// Send queues each client's frames in an outbox that a goroutine of the client's own writes out, so a client that reads slowly or not at all never blocks Send or delays the other clients (see OutboxLimits).
type WebSocketServer struct {
	ln       stdnet.Listener
	serverID PeerID
	origins  []string
	ka       wsKeepAlive

	mu           sync.Mutex
	table        *sessionTable
	inbox        packetQueue
	outLimits    OutboxLimits
	writeTimeout time.Duration // wsWriteTimeout; tests shorten it under mu before clients connect
	closed       bool
	changed      chan struct{}

	outCounters outboxCounters
}

// ListenWebSocket starts a pure-Go RFC 6455 WebSocket server on addr. serverID is the server's own PeerID (1 when zero); clients get the IDs after it. auth decides who may connect and how long a dropped client keeps its ID.
func ListenWebSocket(addr string, serverID PeerID, auth Auth) (*WebSocketServer, error) {
	return listenWebSocket(addr, serverID, auth, nil, nil, serverKeepAlive(auth))
}

// ListenWebSocketTLS starts the server with TLS, so a browser on an HTTPS page can connect with wss:// without a reverse proxy. auth is as for ListenWebSocket. allowedOrigins lists the browser Origins allowed to upgrade; empty allows any, "*" allows all, and an Origin outside the list is refused before the handshake.
func ListenWebSocketTLS(addr string, serverID PeerID, auth Auth, config *tls.Config, allowedOrigins []string) (*WebSocketServer, error) {
	if config == nil {
		return nil, errors.New("net: websocket TLS needs a tls.Config")
	}
	return listenWebSocket(addr, serverID, auth, config, allowedOrigins, serverKeepAlive(auth))
}

// ListenWebSocketTLSFiles is ListenWebSocketTLS with a certificate and key loaded from PEM files.
func ListenWebSocketTLSFiles(addr string, serverID PeerID, auth Auth, certFile, keyFile string, allowedOrigins []string) (*WebSocketServer, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return ListenWebSocketTLS(addr, serverID, auth, &tls.Config{Certificates: []tls.Certificate{cert}}, allowedOrigins)
}

func listenWebSocket(addr string, serverID PeerID, auth Auth, config *tls.Config, allowedOrigins []string, ka wsKeepAlive) (*WebSocketServer, error) {
	if serverID == 0 {
		serverID = 1
	}
	origins, err := normalizeOrigins(allowedOrigins)
	if err != nil {
		return nil, err
	}
	ln, err := stdnet.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if config != nil {
		ln = tls.NewListener(ln, config)
	}
	s := &WebSocketServer{
		ln:           ln,
		serverID:     serverID,
		origins:      origins,
		ka:           ka,
		table:        newSessionTable(auth, serverID, false),
		writeTimeout: wsWriteTimeout,
		changed:      make(chan struct{}, 1),
	}
	go s.acceptLoop()
	return s, nil
}

// normalizeOrigins checks every allowed Origin and stores it as scheme://host, so an entry that names more than an Origin is refused at start-up instead of silently denying every upgrade.
func normalizeOrigins(origins []string) ([]string, error) {
	out := make([]string, 0, len(origins))
	for _, raw := range origins {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "*" {
			out = append(out, "*")
			continue
		}
		u, err := url.Parse(trimmed)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("net: websocket origin %q is not a scheme://host Origin", raw)
		}
		if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("net: websocket origin %q names more than a scheme://host Origin", raw)
		}
		out = append(out, strings.ToLower(u.Scheme+"://"+u.Host))
	}
	return out, nil
}

// originAllowed reports whether a browser Origin matches the normalized allow-list.
func originAllowed(origins []string, origin string) bool {
	if len(origins) == 0 || strings.TrimSpace(origin) == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	normalized := strings.ToLower(u.Scheme + "://" + u.Host)
	for _, allowed := range origins {
		if allowed == "*" || allowed == normalized {
			return true
		}
	}
	return false
}

// originAllowed reports whether a browser Origin may upgrade.
func (s *WebSocketServer) originAllowed(origin string) bool {
	return originAllowed(s.origins, origin)
}

// Addr returns the listener's network address.
func (s *WebSocketServer) Addr() stdnet.Addr {
	return s.ln.Addr()
}

func (s *WebSocketServer) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

// handshakeReader caps the bytes read from a connection until the HTTP upgrade request is parsed; a negative n means unlimited, as it is once the handshake is done.
type handshakeReader struct {
	r io.Reader
	n int64
}

var errHandshakeTooLarge = errors.New("net: websocket handshake request too large")

func (h *handshakeReader) Read(p []byte) (int, error) {
	if h.n < 0 {
		return h.r.Read(p)
	}
	if h.n == 0 {
		return 0, errHandshakeTooLarge
	}
	if int64(len(p)) > h.n {
		p = p[:h.n]
	}
	n, err := h.r.Read(p)
	h.n -= int64(n)
	return n, err
}

func (h *handshakeReader) unlimited() { h.n = -1 }

func (s *WebSocketServer) handleConn(conn stdnet.Conn) {
	// Bound the upgrade and the connection handshake so a client that connects and sends nothing cannot occupy a goroutine and socket forever, and cap the upgrade request's headers so it cannot stream an unbounded request into http.ReadRequest, which enforces no limit of its own.
	_ = conn.SetDeadline(time.Now().Add(wsHandshakeTimeout))
	ir := &wsIdleReader{conn: conn}
	hr := &handshakeReader{r: ir, n: maxWSHandshakeSize}
	br := bufio.NewReader(hr)
	req, err := http.ReadRequest(br)
	if err != nil {
		_ = conn.Close()
		return
	}
	hr.unlimited()
	if !s.originAllowed(req.Header.Get("Origin")) {
		_, _ = io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
		_ = conn.Close()
		return
	}
	secKey := strings.TrimSpace(req.Header.Get("Sec-WebSocket-Key"))
	if secKey == "" || !strings.EqualFold(strings.TrimSpace(req.Header.Get("Upgrade")), "websocket") {
		_, _ = io.WriteString(conn, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n")
		_ = conn.Close()
		return
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + wsAcceptKey(secKey) + "\r\n\r\n"
	if _, err := io.WriteString(conn, resp); err != nil {
		_ = conn.Close()
		return
	}

	fin, op, payload, _, err := readWSFrameLimit(br, wsMaxHelloFrame)
	if err != nil || !fin || op != wsOpBinary {
		_ = conn.Close()
		return
	}
	h, err := decodeHello(payload)
	if err != nil {
		if errors.Is(err, errHandshakeVersion) {
			_ = writeWSFrame(conn, wsOpBinary, false, encodeReject(h.nonce, hsReasonVersion, 0))
		}
		_ = writeWSFrame(conn, wsOpClose, false, nil)
		_ = conn.Close()
		return
	}

	client := &wsClientConn{conn: conn, br: br, out: newOutbox(&s.outCounters), writerDone: make(chan struct{})}
	defer client.out.close()
	// Hold the client's write lock until the welcome is out, so no close frame from Kick or Close overtakes it once the session is visible. Send only queues frames, and the writer that writes them starts after the welcome.
	client.writeMu.Lock()
	reply, ok := s.admit(h, client)
	if reply != nil {
		err = writeWSFrame(conn, wsOpBinary, false, reply)
	}
	if !ok {
		_ = writeWSFrame(conn, wsOpClose, false, nil)
		client.writeMu.Unlock()
		_ = conn.Close()
		return
	}
	client.writeMu.Unlock()
	if err != nil {
		s.clientGone(client, false)
		return
	}
	_ = conn.SetDeadline(time.Time{})
	// A client that sends nothing, not even the pong to the server's pings, for the idle timeout is gone: its network vanished without closing the TCP connection.
	ir.idle = s.ka.idle
	stop := make(chan struct{})
	defer close(stop)
	go wsPingLoop(conn, &client.writeMu, &client.closing, false, s.ka.ping, stop)
	go s.writeClientLoop(client)
	s.readClientLoop(client)
}

// writeClientLoop writes the frames Send queued for a client until its outbox closes, which happens when the connection ends. A write that fails or times out may have left part of a frame on the wire, so it closes the connection instead of writing the next frame after it: the read loop then reports the drop, and no later frame reaches the client misframed. After Kick or Close (see shutdown) it writes the frames still queued, then the close frame, and closes the connection.
func (s *WebSocketServer) writeClientLoop(c *wsClientConn) {
	defer close(c.writerDone)
	for {
		item, ok := c.out.next()
		if !ok {
			return
		}
		if item.final {
			c.writeMu.Lock()
			// Setting closing stops the ping loop, so no frame follows the close frame; it is already set when the flush ran out of time.
			if !c.closing.Swap(true) {
				_ = c.conn.SetWriteDeadline(item.flushBy)
				_ = writeWSFrame(c.conn, wsOpClose, false, item.data)
			}
			c.writeMu.Unlock()
			_ = c.conn.Close()
			return
		}
		deadline := time.Now().Add(c.writeTimeout)
		if !item.flushBy.IsZero() && item.flushBy.Before(deadline) {
			deadline = item.flushBy
		}
		c.writeMu.Lock()
		err := writeWSFrameBy(c.conn, &c.closing, deadline, wsOpBinary, false, item.data)
		c.writeMu.Unlock()
		if err != nil {
			c.out.close()
			_ = c.conn.Close()
			return
		}
	}
}

// admit runs the session side of the handshake and returns the reply for the client, and whether the client is now connected.
func (s *WebSocketServer) admit(h hello, c *wsClientConn) ([]byte, bool) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, false
	}
	now := s.table.now()
	s.table.expire(now)
	if sess := s.table.resume(h, now); sess != nil {
		old, _ := sess.link.(*wsClientConn)
		sess.link = c
		c.id = sess.id
		c.writeTimeout = s.writeTimeout
		reply := encodeWelcome(s.table.welcomeFor(sess, h.nonce, true))
		s.mu.Unlock()
		if old != nil {
			// A half-open connection the server had not noticed yet; its read loop sees it is no longer the session's link and reports nothing.
			_ = old.conn.Close()
		}
		s.notify()
		return reply, true
	}
	id, ok := s.table.reserve()
	s.mu.Unlock()
	if !ok {
		return encodeReject(h.nonce, reasonServerFull, 0), false
	}

	reason, ok := s.table.verify(id, h.token)

	s.mu.Lock()
	defer s.mu.Unlock()
	if !ok || s.closed {
		s.table.release(id)
		if ok {
			reason = "server closed"
		}
		return encodeReject(h.nonce, reason, 0), false
	}
	sess := s.table.commit(id, s.table.now())
	sess.link = c
	c.id = id
	c.writeTimeout = s.writeTimeout
	s.notify()
	return encodeWelcome(s.table.welcomeFor(sess, h.nonce, false)), true
}

// clientGone ends a client's connection: a close frame is a leave unless it carries wsCloseReconnect, and anything else is a drop that keeps the session for the grace period. A connection that a resume already replaced reports nothing.
func (s *WebSocketServer) clientGone(c *wsClientConn, goodbye bool) {
	s.mu.Lock()
	if !s.closed {
		if sess := s.table.sessions[c.id]; sess != nil && sess.link == c {
			if goodbye {
				s.table.leave(sess)
			} else {
				s.table.drop(sess, s.table.now())
			}
		}
	}
	s.mu.Unlock()
	_ = c.conn.Close()
	s.notify()
}

func (s *WebSocketServer) readClientLoop(c *wsClientConn) {
	goodbye := false
	defer func() { s.clientGone(c, goodbye) }()

	var fragBuf []byte
	var fragOp byte
	inFrag := false

	for {
		fin, op, payload, _, err := readWSFrame(c.br)
		if err != nil {
			return
		}
		switch op {
		case wsOpPing:
			c.writeMu.Lock()
			err := writeWSFrameDeadline(c.conn, &c.closing, wsOpPong, false, payload)
			c.writeMu.Unlock()
			if err != nil {
				return // a pong that failed or timed out may have left part of a frame on the wire, so end the connection
			}
		case wsOpPong:
			continue
		case wsOpClose:
			c.writeMu.Lock()
			_ = writeWSFrameDeadline(c.conn, &c.closing, wsOpClose, false, nil)
			c.writeMu.Unlock()
			// A browser cannot drop its socket without a close frame, so a client that closes to reconnect says so with its close code.
			goodbye = wsCloseCode(payload) != wsCloseReconnect
			return
		case wsOpBinary, wsOpText:
			if !fin {
				fragOp = op
				fragBuf = append(fragBuf[:0], payload...)
				inFrag = true
				continue
			}
			if op == wsOpBinary && !s.deliverFromClient(c, payload) {
				goodbye = true // past its reliable inbox cap: end the session so its reliable stream never resumes with a gap
				return
			}
		case wsOpContinuation:
			if !inFrag {
				continue
			}
			if len(fragBuf)+len(payload) > maxWSMessageSize {
				return
			}
			fragBuf = append(fragBuf, payload...)
			if fin {
				if fragOp == wsOpBinary {
					cp := make([]byte, len(fragBuf))
					copy(cp, fragBuf)
					if !s.deliverFromClient(c, cp) {
						goodbye = true
						return
					}
				}
				fragBuf = fragBuf[:0]
				inFrag = false
			}
		}
	}
}

// wsClosePayload builds a close frame's payload: the status code followed by the reason, truncated so the payload fits a control frame.
func wsClosePayload(code uint16, reason string) []byte {
	reason = truncateReason(reason, wsMaxControlPayload-2)
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload, code)
	copy(payload[2:], reason)
	return payload
}

// wsCloseCode returns the status code of a close frame's payload, or 0 when it has none.
func wsCloseCode(payload []byte) uint16 {
	if len(payload) < 2 {
		return 0
	}
	return binary.BigEndian.Uint16(payload)
}

// deliverFromClient queues one of a client's frames. It returns false when the client is past its reliable inbox cap (InboxLimits.Reliable), and the caller then ends the client's session.
func (s *WebSocketServer) deliverFromClient(c *wsClientConn, frame []byte) bool {
	ch, seq, payload, err := decodeWireFrame(frame)
	if err != nil {
		return true
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	ok := true
	s.mu.Lock()
	if sess := s.table.sessions[c.id]; !s.closed && sess != nil && sess.link == c {
		ok = s.inbox.admit(Packet{
			From:    c.id,
			Channel: ch,
			Payload: cp,
			Seq:     seq,
		})
	}
	s.mu.Unlock()
	s.notify()
	return ok
}

// notify wakes a waiter on changed without blocking.
func (s *WebSocketServer) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// Send queues payload for a connected client (or all clients when to == PeerBroadcast) and returns without waiting for the connection: each client's own goroutine writes its frames in order (see OutboxLimits). It returns ErrDisconnected for a client inside its grace period or whose session a full outbox has just ended, ErrUnknownPeer for an ID with no session, and ErrPayloadTooLarge for a payload that does not fit a WebSocket message. A write that later fails or times out is not returned by any Send: NextEvent reports it as PeerDropped.
func (s *WebSocketServer) Send(to PeerID, ch Channel, payload []byte) error {
	if ch != ChannelReliableOrdered && ch != ChannelUnreliable {
		return ErrInvalidPacket
	}
	if wireHeaderSize+len(payload) > maxWSMessageSize {
		return ErrPayloadTooLarge
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	var targets []*wsClientConn
	if to == PeerBroadcast {
		for _, id := range s.table.connectedIDs() {
			targets = append(targets, s.table.sessions[id].link.(*wsClientConn))
		}
	} else {
		if err := s.table.sendable(to); err != nil {
			s.mu.Unlock()
			return err
		}
		targets = []*wsClientConn{s.table.sessions[to].link.(*wsClientConn)}
	}
	limits := s.outLimits
	s.mu.Unlock()

	var firstErr error
	for _, c := range targets {
		var err error
		switch c.out.push(ch, payload, limits, encodeWireFrame) {
		case pushOverflow:
			s.endOverflowed(c)
			err = ErrDisconnected
		case pushClosed:
			err = ErrDisconnected
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// endOverflowed ends the session of a client whose reliable frame did not fit its outbox, as if it had left, so its reliable stream never resumes with a gap. It does not wait on the connection: it breaks a write in progress and closes the connection on a goroutine of its own, because closing a TLS connection writes an alert to a client that may not be reading.
func (s *WebSocketServer) endOverflowed(c *wsClientConn) {
	s.mu.Lock()
	if sess := s.table.sessions[c.id]; !s.closed && sess != nil && sess.link == c {
		s.table.leaveReason(sess, reasonOutboxFull)
		s.outCounters.overflows.Add(1)
	}
	s.mu.Unlock()
	c.out.close()
	c.closing.Store(true)
	_ = c.conn.SetWriteDeadline(time.Now())
	go func() { _ = c.conn.Close() }()
	s.notify()
}

// Kick ends peer's session now: a connected client receives the frames already sent to it, then a close frame with the wsCloseKicked code and the reason, and the session's resume secret is gone, so the client's Reconnect cannot resume it and joins as a new peer only if Auth.Verify accepts its token again. NextEvent reports PeerLeft with the reason. It returns ErrUnknownPeer for an ID with no session (including a handshake still in Verify) and ErrClosed once the server is closed. Kick does not wait for the connection: the client's own goroutine writes the queued frames and the close frame, and whatever a client that reads too slowly has not taken within a second is cut off with the connection.
func (s *WebSocketServer) Kick(peer PeerID, reason string) error {
	reason = truncateReason(reason, hsMaxReason)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	sess := s.table.sessions[peer]
	if sess == nil || sess.pending {
		s.mu.Unlock()
		return ErrUnknownPeer
	}
	link, _ := sess.link.(*wsClientConn)
	s.table.leaveReason(sess, reason)
	s.mu.Unlock()
	s.notify()
	if link == nil {
		// The peer was inside its grace period: nothing to notify it on; it learns the kick as a new handshake when it reconnects.
		return nil
	}
	link.shutdown(wsClosePayload(wsCloseKicked, reason), time.Now().Add(wsCloseFlushTimeout))
	return nil
}

// SetInboxLimits bounds the packets the server keeps until Recv takes them (see InboxLimits). A client whose queued reliable packets would pass InboxLimits.Reliable has its session ended, as if it had left.
func (s *WebSocketServer) SetInboxLimits(l InboxLimits) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inbox.setLimits(l)
}

// InboxStats reports what the server's inbox holds and what its limits dropped or warned about.
func (s *WebSocketServer) InboxStats() InboxStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inbox.stats()
}

// SetOutboxLimits bounds the frames the server holds for each client before they are written (see OutboxLimits).
func (s *WebSocketServer) SetOutboxLimits(l OutboxLimits) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outLimits = l
}

// OutboxStats reports what the connected clients' outboxes hold and what their limits dropped.
func (s *WebSocketServer) OutboxStats() OutboxStats {
	s.mu.Lock()
	var st OutboxStats
	for _, sess := range s.table.sessions {
		if c, ok := sess.link.(*wsClientConn); ok {
			frames, bytes := c.out.stats()
			st.Frames += frames
			st.Bytes += bytes
		}
	}
	s.mu.Unlock()
	st.UnreliableDropped = s.outCounters.dropped.Load()
	st.Overflows = s.outCounters.overflows.Load()
	return st
}

// Recv pops the next received packet from any connected client without blocking.
func (s *WebSocketServer) Recv() (Packet, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inbox.pop()
}

// NextEvent pops the next join, drop, resume or leave without blocking, after ending the sessions whose grace period has passed.
func (s *WebSocketServer) NextEvent() (PeerEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.table.expire(s.table.now())
	return s.table.popEvent()
}

// LocalID returns the server's PeerID.
func (s *WebSocketServer) LocalID() PeerID {
	return s.serverID
}

// Peers returns the sorted list of currently connected client PeerIDs. Clients inside their grace period are left out.
func (s *WebSocketServer) Peers() []PeerID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.table.expire(s.table.now())
	return s.table.connectedIDs()
}

// Close stops accepting new clients and closes all active client connections. Each client receives the frames already sent to it and then a close frame, all clients at once; Close waits at most a second for them and cuts off the connections of clients that read too slowly to take theirs by then.
func (s *WebSocketServer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	var clients []*wsClientConn
	for _, sess := range s.table.sessions {
		if c, ok := sess.link.(*wsClientConn); ok {
			clients = append(clients, c)
		}
	}
	s.mu.Unlock()

	// Every client's writer flushes what Send queued and then the close frame, all at once; Close waits for them for at most wsCloseFlushTimeout, when shutdown cuts off the clients that have not taken theirs.
	flushBy := time.Now().Add(wsCloseFlushTimeout)
	for _, c := range clients {
		c.shutdown(nil, flushBy)
	}
	timer := time.NewTimer(time.Until(flushBy))
	defer timer.Stop()
wait:
	for _, c := range clients {
		select {
		case <-c.writerDone:
		case <-timer.C:
			break wait
		}
	}
	return s.ln.Close()
}

// relayWake lets a Relay serving this server wait for packets and events instead of polling.
func (s *WebSocketServer) relayWake() <-chan struct{} {
	return s.changed
}
