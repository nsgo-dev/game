//go:build !js

package net

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
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
	"time"
)

const (
	wsMagicGUID        = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	maxWSMessageSize   = 4 << 20 // 4 MiB safety cap
	maxWSHandshakeSize = 1 << 20 // 1 MiB cap on the HTTP upgrade request's headers
	wsWriteTimeout     = 10 * time.Second
	wsDialTimeout      = 10 * time.Second
	wsHandshakeTimeout = 10 * time.Second

	// wsEagerAlloc is the largest payload readWSFrame allocates before its bytes arrive. A larger payload grows with the bytes actually received, so a peer that announces a 4 MiB frame and then trickles it (a slow-loris attack) holds memory only for what it has sent (#546).
	wsEagerAlloc = 64 << 10
	// wsMaxHelloFrame bounds the first frame a server reads from a client, which is the hello: its header and a token of at most MaxTokenSize bytes.
	wsMaxHelloFrame = hsHelloHeader + MaxTokenSize
	// wsMaxReplyFrame bounds the welcome or reject a client reads in the handshake.
	wsMaxReplyFrame = 1 << 10

	// wsMinIdle is the shortest Auth.IdleTimeout a WebSocket server uses, as for UDP.
	wsMinIdle = 3 * time.Second
	// wsClientPing is how often a native client pings the server, and wsClientIdle how long it waits for any frame before it counts the server as gone. Pinging much more often than the idle timeout keeps a healthy connection alive whatever interval the server pings at.
	wsClientPing = 2 * time.Second
	wsClientIdle = 10 * time.Second

	wsOpContinuation = 0x0
	wsOpText         = 0x1
	wsOpBinary       = 0x2
	wsOpClose        = 0x8
	wsOpPing         = 0x9
	wsOpPong         = 0xA
)

// readWSPayload reads exactly n payload bytes. Up to wsEagerAlloc it allocates them at once; past that the buffer grows as bytes arrive instead of trusting the length the peer announced.
func readWSPayload(r io.Reader, n uint64) ([]byte, error) {
	if n <= wsEagerAlloc {
		buf := make([]byte, n)
		_, err := io.ReadFull(r, buf)
		return buf, err
	}
	var buf bytes.Buffer
	buf.Grow(wsEagerAlloc)
	if _, err := io.CopyN(&buf, r, int64(n)); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return buf.Bytes(), nil
}

func wsAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(strings.TrimSpace(key)))
	h.Write([]byte(wsMagicGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func writeWSFrameDeadline(conn stdnet.Conn, opcode byte, masked bool, payload []byte) error {
	_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	err := writeWSFrame(conn, opcode, masked, payload)
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

func writeWSFrame(w io.Writer, opcode byte, masked bool, payload []byte) error {
	if len(payload) > maxWSMessageSize {
		return ErrPayloadTooLarge
	}
	var header [14]byte
	header[0] = 0x80 | (opcode & 0x0F) // FIN=1

	var maskBit byte
	if masked {
		maskBit = 0x80
	}

	n := 2
	plen := len(payload)
	switch {
	case plen < 126:
		header[1] = maskBit | byte(plen)
	case plen <= 0xFFFF:
		header[1] = maskBit | 126
		binary.BigEndian.PutUint16(header[2:4], uint16(plen))
		n = 4
	default:
		header[1] = maskBit | 127
		binary.BigEndian.PutUint64(header[2:10], uint64(plen))
		n = 10
	}

	if masked {
		var maskKey [4]byte
		if _, err := rand.Read(maskKey[:]); err != nil {
			return err
		}
		copy(header[n:n+4], maskKey[:])
		n += 4
		buf := make([]byte, n+plen)
		copy(buf[:n], header[:n])
		for i := range plen {
			buf[n+i] = payload[i] ^ maskKey[i&3]
		}
		_, err := w.Write(buf)
		return err
	}

	if _, err := w.Write(header[:n]); err != nil {
		return err
	}
	if plen > 0 {
		_, err := w.Write(payload)
		return err
	}
	return nil
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

// wsPingLoop pings the peer every ka.ping until stop closes. A ping that cannot be written closes the connection, so the read loop ends and reports the drop.
func wsPingLoop(conn stdnet.Conn, writeMu *sync.Mutex, masked bool, every time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			writeMu.Lock()
			err := writeWSFrameDeadline(conn, wsOpPing, masked, nil)
			writeMu.Unlock()
			if err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}

// readWSFrame reads one frame of at most maxWSMessageSize bytes.
func readWSFrame(r *bufio.Reader) (fin bool, opcode byte, payload []byte, masked bool, err error) {
	return readWSFrameLimit(r, maxWSMessageSize)
}

// readWSFrameLimit reads one frame whose payload is at most limit bytes.
func readWSFrameLimit(r *bufio.Reader, limit uint64) (fin bool, opcode byte, payload []byte, masked bool, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return false, 0, nil, false, err
	}
	fin = (hdr[0] & 0x80) != 0
	opcode = hdr[0] & 0x0F
	masked = (hdr[1] & 0x80) != 0
	plen := uint64(hdr[1] & 0x7F)

	switch plen {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return false, 0, nil, false, err
		}
		plen = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return false, 0, nil, false, err
		}
		plen = binary.BigEndian.Uint64(ext[:])
	}

	if plen > limit {
		return false, 0, nil, false, ErrPayloadTooLarge
	}

	var maskKey [4]byte
	if masked {
		if _, err = io.ReadFull(r, maskKey[:]); err != nil {
			return false, 0, nil, false, err
		}
	}

	if payload, err = readWSPayload(r, plen); err != nil {
		return false, 0, nil, false, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i&3]
		}
	}
	return fin, opcode, payload, masked, nil
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

// webSocketHostPort joins the URL's host and port, defaulting by scheme and
// keeping IPv6 literals bracketed; checking for a colon cannot tell a port from
// an IPv6 address (#546).
func webSocketHostPort(u *url.URL) (string, error) {
	host := u.Hostname()
	if host == "" {
		return "", errors.New("net: websocket url has no host")
	}
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "ws":
			port = "80"
		case "wss":
			port = "443"
		default:
			return "", fmt.Errorf("net: unsupported websocket scheme %q", u.Scheme)
		}
	}
	return stdnet.JoinHostPort(host, port), nil
}

// DialWebSocket opens an RFC 6455 WebSocket connection to rawURL (ws:// or wss://), presents token in the connection handshake and returns a Transport ready for non-blocking Send/Recv once the server accepts it. The server assigns the client's ID, which LocalID reports. A refusal returns a *RejectError that matches ErrRejected.
//
// The client pings the server every 2 seconds and counts the connection as dropped, reporting ConnDisconnected, when no frame arrives for 10 seconds.
func DialWebSocket(rawURL string, token []byte) (*WebSocketTransport, error) {
	return dialWebSocket(rawURL, token, clientKeepAlive)
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

	conn, br, err := dialWebSocketConn(wt.rawURL)
	if err != nil {
		return wt.fail(err)
	}
	w, err := clientHandshake(conn, br, wt.token, res)
	if err != nil {
		_ = conn.Close()
		return wt.fail(err)
	}
	_ = conn.SetDeadline(time.Time{})

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
	go wsPingLoop(conn, &wt.writeMu, true, wt.ka.ping, stop)
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

// dialWebSocketConn opens the TCP (or TLS) connection and performs the HTTP upgrade. The returned connection still has the handshake deadline set.
func dialWebSocketConn(rawURL string) (stdnet.Conn, *bufio.Reader, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("net: parse websocket url: %w", err)
	}
	host, err := webSocketHostPort(u)
	if err != nil {
		return nil, nil, err
	}

	dialer := &stdnet.Dialer{Timeout: wsDialTimeout}
	var conn stdnet.Conn
	if u.Scheme == "wss" {
		conn, err = tls.DialWithDialer(dialer, "tcp", host, &tls.Config{ServerName: u.Hostname()})
	} else {
		conn, err = dialer.Dial("tcp", host)
	}
	if err != nil {
		return nil, nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(wsHandshakeTimeout))

	var keyBytes [16]byte
	_, _ = rand.Read(keyBytes[:]) // crypto/rand.Read never fails since Go 1.24
	secKey := base64.StdEncoding.EncodeToString(keyBytes[:])

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}

	var reqBuf strings.Builder
	fmt.Fprintf(&reqBuf, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(&reqBuf, "Host: %s\r\n", u.Host)
	reqBuf.WriteString("Upgrade: websocket\r\n")
	reqBuf.WriteString("Connection: Upgrade\r\n")
	fmt.Fprintf(&reqBuf, "Sec-WebSocket-Key: %s\r\n", secKey)
	reqBuf.WriteString("Sec-WebSocket-Version: 13\r\n")
	reqBuf.WriteString("\r\n")

	if _, err := io.WriteString(conn, reqBuf.String()); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("net: websocket handshake read: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("net: websocket upgrade rejected with status %d", resp.StatusCode)
	}
	if strings.TrimSpace(resp.Header.Get("Sec-WebSocket-Accept")) != wsAcceptKey(secKey) {
		_ = conn.Close()
		return nil, nil, errors.New("net: websocket invalid Sec-WebSocket-Accept")
	}
	return conn, br, nil
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
		// Any frame, the server's pongs and pings included, proves the connection is alive.
		_ = conn.SetReadDeadline(time.Now().Add(wt.ka.idle))
		fin, op, payload, _, err := readWSFrame(br)
		if err != nil {
			cause = fmt.Errorf("%w: %w", ErrDisconnected, err)
			return
		}
		switch op {
		case wsOpPing:
			wt.writeMu.Lock()
			_ = writeWSFrameDeadline(conn, wsOpPong, true, payload)
			wt.writeMu.Unlock()
		case wsOpPong:
			continue
		case wsOpClose:
			wt.writeMu.Lock()
			_ = writeWSFrameDeadline(conn, wsOpClose, true, nil)
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
	return writeWSFrameDeadline(conn, wsOpBinary, true, frame)
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

	// Break a Send already blocked on a client that does not read, then send
	// the close frame with its own short deadline.
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
	seq     [2]uint32
}

// WebSocketServer accepts RFC 6455 WebSocket clients and exposes them through the standard Transport interface.
//
// Every client runs the connection handshake before its packets reach Recv: Auth.Verify checks its token, the server assigns its ID and tells the client, and NextEvent reports joins, drops, resumes and leaves.
//
// The server pings every client and reports PeerDropped for one that stays silent for Auth.IdleTimeout. Browsers answer pings on their own, so browser clients need no code for it.
type WebSocketServer struct {
	ln       stdnet.Listener
	serverID PeerID
	origins  []string
	ka       wsKeepAlive

	mu      sync.Mutex
	table   *sessionTable
	inbox   packetQueue
	closed  bool
	changed chan struct{}
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
		ln:       ln,
		serverID: serverID,
		origins:  origins,
		ka:       ka,
		table:    newSessionTable(auth, serverID, false),
		changed:  make(chan struct{}, 1),
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

// originAllowed reports whether a browser Origin may upgrade.
func (s *WebSocketServer) originAllowed(origin string) bool {
	if len(s.origins) == 0 || strings.TrimSpace(origin) == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	normalized := strings.ToLower(u.Scheme + "://" + u.Host)
	for _, allowed := range s.origins {
		if allowed == "*" || allowed == normalized {
			return true
		}
	}
	return false
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
	hr := &handshakeReader{r: conn, n: maxWSHandshakeSize}
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

	client := &wsClientConn{conn: conn, br: br}
	// Hold the client's write lock until the welcome is out, so no game packet overtakes it once the session is visible to Send.
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
	stop := make(chan struct{})
	defer close(stop)
	go wsPingLoop(conn, &client.writeMu, false, s.ka.ping, stop)
	s.readClientLoop(client)
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
		// A client that sends nothing, not even the pong to the server's pings, for the idle timeout is gone: its network vanished without closing the TCP connection.
		_ = c.conn.SetReadDeadline(time.Now().Add(s.ka.idle))
		fin, op, payload, _, err := readWSFrame(c.br)
		if err != nil {
			return
		}
		switch op {
		case wsOpPing:
			c.writeMu.Lock()
			_ = writeWSFrameDeadline(c.conn, wsOpPong, false, payload)
			c.writeMu.Unlock()
		case wsOpPong:
			continue
		case wsOpClose:
			c.writeMu.Lock()
			_ = writeWSFrameDeadline(c.conn, wsOpClose, false, nil)
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
			if op == wsOpBinary {
				s.deliverFromClient(c, payload)
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
					s.deliverFromClient(c, cp)
				}
				fragBuf = fragBuf[:0]
				inFrag = false
			}
		}
	}
}

// wsCloseCode returns the status code of a close frame's payload, or 0 when it has none.
func wsCloseCode(payload []byte) uint16 {
	if len(payload) < 2 {
		return 0
	}
	return binary.BigEndian.Uint16(payload)
}

func (s *WebSocketServer) deliverFromClient(c *wsClientConn, frame []byte) {
	ch, seq, payload, err := decodeWireFrame(frame)
	if err != nil {
		return
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	s.mu.Lock()
	if sess := s.table.sessions[c.id]; !s.closed && sess != nil && sess.link == c {
		s.inbox.push(Packet{
			From:    c.id,
			Channel: ch,
			Payload: cp,
			Seq:     seq,
		})
	}
	s.mu.Unlock()
	s.notify()
}

// notify wakes a waiter on changed without blocking.
func (s *WebSocketServer) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// Send transmits payload to a connected client (or all clients when to == PeerBroadcast). It returns ErrDisconnected for a client inside its grace period and ErrUnknownPeer for an ID with no session.
func (s *WebSocketServer) Send(to PeerID, ch Channel, payload []byte) error {
	if ch != ChannelReliableOrdered && ch != ChannelUnreliable {
		return ErrInvalidPacket
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
	s.mu.Unlock()

	var firstErr error
	for _, c := range targets {
		c.writeMu.Lock()
		c.seq[ch]++
		frame := encodeWireFrame(ch, c.seq[ch], payload)
		err := writeWSFrameDeadline(c.conn, wsOpBinary, false, frame)
		c.writeMu.Unlock()
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
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

// Close stops accepting new clients and closes all active client connections.
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

	for _, c := range clients {
		c.writeMu.Lock()
		_ = writeWSFrameDeadline(c.conn, wsOpClose, false, nil)
		c.writeMu.Unlock()
		_ = c.conn.Close()
	}
	return s.ln.Close()
}

// relayWake lets a Relay serving this server wait for packets and events instead of polling.
func (s *WebSocketServer) relayWake() <-chan struct{} {
	return s.changed
}
