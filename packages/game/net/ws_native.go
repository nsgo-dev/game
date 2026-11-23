//go:build !js

package net

import (
	"bufio"
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
	"strconv"
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

	wsOpContinuation = 0x0
	wsOpText         = 0x1
	wsOpBinary       = 0x2
	wsOpClose        = 0x8
	wsOpPing         = 0x9
	wsOpPong         = 0xA
)

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

func readWSFrame(r *bufio.Reader) (fin bool, opcode byte, payload []byte, masked bool, err error) {
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

	if plen > maxWSMessageSize {
		return false, 0, nil, false, ErrPayloadTooLarge
	}

	var maskKey [4]byte
	if masked {
		if _, err = io.ReadFull(r, maskKey[:]); err != nil {
			return false, 0, nil, false, err
		}
	}

	payload = make([]byte, plen)
	if plen > 0 {
		if _, err = io.ReadFull(r, payload); err != nil {
			return false, 0, nil, false, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= maskKey[i&3]
			}
		}
	}
	return fin, opcode, payload, masked, nil
}

// WebSocketTransport is a client-side RFC 6455 WebSocket Transport.
// Binary WebSocket frames carry the 5-byte channel+seq wire header.
type WebSocketTransport struct {
	conn     stdnet.Conn
	br       *bufio.Reader
	localID  PeerID
	serverID PeerID

	writeMu sync.Mutex
	seq     [2]uint32

	mu     sync.Mutex
	inbox  []Packet
	closed bool
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

// DialWebSocket opens an RFC 6455 WebSocket connection to rawURL (ws:// or wss://)
// and returns a Transport ready for non-blocking Send/Recv.
func DialWebSocket(rawURL string, localID PeerID) (*WebSocketTransport, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("net: parse websocket url: %w", err)
	}
	host, err := webSocketHostPort(u)
	if err != nil {
		return nil, err
	}

	dialer := &stdnet.Dialer{Timeout: wsDialTimeout}
	var conn stdnet.Conn
	if u.Scheme == "wss" {
		conn, err = tls.DialWithDialer(dialer, "tcp", host, &tls.Config{ServerName: u.Hostname()})
	} else {
		conn, err = dialer.Dial("tcp", host)
	}
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(wsHandshakeTimeout))

	var keyBytes [16]byte
	if _, err := rand.Read(keyBytes[:]); err != nil {
		_ = conn.Close()
		return nil, err
	}
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
	if localID != 0 {
		fmt.Fprintf(&reqBuf, "X-NSGo-Peer-ID: %d\r\n", localID)
	}
	reqBuf.WriteString("\r\n")

	if _, err := io.WriteString(conn, reqBuf.String()); err != nil {
		_ = conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("net: websocket handshake read: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, fmt.Errorf("net: websocket upgrade rejected with status %d", resp.StatusCode)
	}
	expectedAccept := wsAcceptKey(secKey)
	if strings.TrimSpace(resp.Header.Get("Sec-WebSocket-Accept")) != expectedAccept {
		_ = conn.Close()
		return nil, errors.New("net: websocket invalid Sec-WebSocket-Accept")
	}

	serverID := PeerID(1)
	if v := strings.TrimSpace(resp.Header.Get("X-NSGo-Server-ID")); v != "" {
		if parsed, parseErr := strconv.ParseUint(v, 10, 32); parseErr == nil && parsed != 0 {
			serverID = PeerID(parsed)
		}
	}
	if localID == 0 {
		if v := strings.TrimSpace(resp.Header.Get("X-NSGo-Peer-ID")); v != "" {
			if parsed, parseErr := strconv.ParseUint(v, 10, 32); parseErr == nil {
				localID = PeerID(parsed)
			}
		}
	}

	_ = conn.SetDeadline(time.Time{})
	wt := &WebSocketTransport{
		conn:     conn,
		br:       br,
		localID:  localID,
		serverID: serverID,
	}
	go wt.readLoop()
	return wt, nil
}

func (wt *WebSocketTransport) readLoop() {
	defer func() {
		wt.mu.Lock()
		wt.closed = true
		wt.mu.Unlock()
		_ = wt.conn.Close()
	}()

	var fragBuf []byte
	var fragOp byte
	inFrag := false

	for {
		fin, op, payload, _, err := readWSFrame(wt.br)
		if err != nil {
			return
		}
		switch op {
		case wsOpPing:
			wt.writeMu.Lock()
			_ = writeWSFrameDeadline(wt.conn, wsOpPong, true, payload)
			wt.writeMu.Unlock()
		case wsOpPong:
			continue
		case wsOpClose:
			wt.writeMu.Lock()
			_ = writeWSFrameDeadline(wt.conn, wsOpClose, true, nil)
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
				wt.deliverBinary(payload)
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
					wt.deliverBinary(cp)
				}
				fragBuf = fragBuf[:0]
				inFrag = false
			}
		}
	}
}

func (wt *WebSocketTransport) deliverBinary(frame []byte) {
	ch, seq, payload, err := decodeWireFrame(frame)
	if err != nil {
		return
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	wt.mu.Lock()
	if !wt.closed {
		wt.inbox = append(wt.inbox, Packet{
			From:    wt.serverID,
			Channel: ch,
			Payload: cp,
			Seq:     seq,
		})
	}
	wt.mu.Unlock()
}

// Send frames payload with the 5-byte channel+seq header and sends it as a masked
// RFC 6455 binary WebSocket frame to the server.
func (wt *WebSocketTransport) Send(to PeerID, ch Channel, payload []byte) error {
	wt.mu.Lock()
	closed := wt.closed
	wt.mu.Unlock()
	if closed {
		return ErrClosed
	}
	if to != PeerBroadcast && to != wt.serverID {
		return ErrUnknownPeer
	}
	if ch != ChannelReliableOrdered && ch != ChannelUnreliable {
		return ErrInvalidPacket
	}

	wt.writeMu.Lock()
	defer wt.writeMu.Unlock()
	wt.seq[ch]++
	frame := encodeWireFrame(ch, wt.seq[ch], payload)
	return writeWSFrameDeadline(wt.conn, wsOpBinary, true, frame)
}

// Recv pops the next received packet without blocking.
func (wt *WebSocketTransport) Recv() (Packet, bool) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if len(wt.inbox) == 0 {
		return Packet{}, false
	}
	pkt := wt.inbox[0]
	copy(wt.inbox, wt.inbox[1:])
	wt.inbox = wt.inbox[:len(wt.inbox)-1]
	return pkt, true
}

// LocalID returns this client's PeerID.
func (wt *WebSocketTransport) LocalID() PeerID {
	return wt.localID
}

// Peers returns the connected server's PeerID, or nil once closed.
func (wt *WebSocketTransport) Peers() []PeerID {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if wt.closed {
		return nil
	}
	return []PeerID{wt.serverID}
}

// Close sends a WebSocket close frame and closes the underlying connection.
func (wt *WebSocketTransport) Close() error {
	wt.mu.Lock()
	if wt.closed {
		wt.mu.Unlock()
		return nil
	}
	wt.closed = true
	wt.mu.Unlock()

	// Break a Send already blocked on a client that does not read, then send
	// the close frame with its own short deadline.
	_ = wt.conn.SetWriteDeadline(time.Now())
	wt.writeMu.Lock()
	_ = wt.conn.SetWriteDeadline(time.Now().Add(time.Second))
	_ = writeWSFrame(wt.conn, wsOpClose, true, nil)
	wt.writeMu.Unlock()
	return wt.conn.Close()
}

type wsClientConn struct {
	id      PeerID
	conn    stdnet.Conn
	br      *bufio.Reader
	writeMu sync.Mutex
	seq     [2]uint32
}

// WebSocketServer accepts RFC 6455 WebSocket clients and exposes them through
// the standard Transport interface.
type WebSocketServer struct {
	ln       stdnet.Listener
	serverID PeerID
	origins  []string

	mu         sync.Mutex
	nextPeerID PeerID
	clients    map[PeerID]*wsClientConn
	inbox      []Packet
	closed     bool
}

// ListenWebSocket starts a pure-Go RFC 6455 WebSocket server on addr.
func ListenWebSocket(addr string, serverID PeerID) (*WebSocketServer, error) {
	return listenWebSocket(addr, serverID, nil, nil)
}

// ListenWebSocketTLS starts the server with TLS, so a browser on an HTTPS page
// can connect with wss:// without a reverse proxy. allowedOrigins lists the
// browser Origins allowed to upgrade; empty allows any, "*" allows all, and
// an Origin outside the list is refused before the handshake.
func ListenWebSocketTLS(addr string, serverID PeerID, config *tls.Config, allowedOrigins []string) (*WebSocketServer, error) {
	if config == nil {
		return nil, errors.New("net: websocket TLS needs a tls.Config")
	}
	return listenWebSocket(addr, serverID, config, allowedOrigins)
}

// ListenWebSocketTLSFiles is ListenWebSocketTLS with a certificate and key
// loaded from PEM files.
func ListenWebSocketTLSFiles(addr string, serverID PeerID, certFile, keyFile string, allowedOrigins []string) (*WebSocketServer, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return ListenWebSocketTLS(addr, serverID, &tls.Config{Certificates: []tls.Certificate{cert}}, allowedOrigins)
}

func listenWebSocket(addr string, serverID PeerID, config *tls.Config, allowedOrigins []string) (*WebSocketServer, error) {
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
		ln:         ln,
		serverID:   serverID,
		origins:    origins,
		nextPeerID: serverID + 1,
		clients:    make(map[PeerID]*wsClientConn),
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
	// Bound the handshake so a client that connects and sends nothing cannot
	// occupy a goroutine and socket forever, and cap its headers so it cannot
	// stream an unbounded request into http.ReadRequest, which enforces no
	// limit of its own.
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	hr := &handshakeReader{r: conn, n: maxWSHandshakeSize}
	br := bufio.NewReader(hr)
	req, err := http.ReadRequest(br)
	if err != nil {
		_ = conn.Close()
		return
	}
	hr.unlimited()
	_ = conn.SetReadDeadline(time.Time{})
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

	var requestedID PeerID
	if v := strings.TrimSpace(req.Header.Get("X-NSGo-Peer-ID")); v != "" {
		if parsed, parseErr := strconv.ParseUint(v, 10, 32); parseErr == nil && parsed != 0 {
			requestedID = PeerID(parsed)
		}
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = conn.Close()
		return
	}
	peerID := requestedID
	if peerID == 0 || peerID == s.serverID || s.clients[peerID] != nil {
		for s.nextPeerID == 0 || s.nextPeerID == s.serverID || s.clients[s.nextPeerID] != nil {
			s.nextPeerID++
		}
		peerID = s.nextPeerID
		s.nextPeerID++
	}
	client := &wsClientConn{
		id:   peerID,
		conn: conn,
		br:   br,
	}
	s.clients[peerID] = client
	s.mu.Unlock()

	accept := wsAcceptKey(secKey)
	resp := fmt.Sprintf(
		"HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: %s\r\n"+
			"X-NSGo-Server-ID: %d\r\n"+
			"X-NSGo-Peer-ID: %d\r\n\r\n",
		accept, s.serverID, peerID,
	)
	client.writeMu.Lock()
	_, err = io.WriteString(conn, resp)
	client.writeMu.Unlock()
	if err != nil {
		s.removeClient(peerID)
		_ = conn.Close()
		return
	}

	s.readClientLoop(client)
}

func (s *WebSocketServer) removeClient(id PeerID) {
	s.mu.Lock()
	delete(s.clients, id)
	s.mu.Unlock()
}

func (s *WebSocketServer) readClientLoop(c *wsClientConn) {
	defer func() {
		s.removeClient(c.id)
		_ = c.conn.Close()
	}()

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
			_ = writeWSFrameDeadline(c.conn, wsOpPong, false, payload)
			c.writeMu.Unlock()
		case wsOpPong:
			continue
		case wsOpClose:
			c.writeMu.Lock()
			_ = writeWSFrameDeadline(c.conn, wsOpClose, false, nil)
			c.writeMu.Unlock()
			return
		case wsOpBinary, wsOpText:
			if !fin {
				fragOp = op
				fragBuf = append(fragBuf[:0], payload...)
				inFrag = true
				continue
			}
			if op == wsOpBinary {
				s.deliverFromClient(c.id, payload)
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
					s.deliverFromClient(c.id, cp)
				}
				fragBuf = fragBuf[:0]
				inFrag = false
			}
		}
	}
}

func (s *WebSocketServer) deliverFromClient(from PeerID, frame []byte) {
	ch, seq, payload, err := decodeWireFrame(frame)
	if err != nil {
		return
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	s.mu.Lock()
	if !s.closed {
		s.inbox = append(s.inbox, Packet{
			From:    from,
			Channel: ch,
			Payload: cp,
			Seq:     seq,
		})
	}
	s.mu.Unlock()
}

// Send transmits payload to a connected client (or all clients when to == PeerBroadcast).
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
		targets = make([]*wsClientConn, 0, len(s.clients))
		for _, c := range s.clients {
			targets = append(targets, c)
		}
	} else {
		c, ok := s.clients[to]
		if !ok {
			s.mu.Unlock()
			return ErrUnknownPeer
		}
		targets = []*wsClientConn{c}
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
	if len(s.inbox) == 0 {
		return Packet{}, false
	}
	pkt := s.inbox[0]
	copy(s.inbox, s.inbox[1:])
	s.inbox = s.inbox[:len(s.inbox)-1]
	return pkt, true
}

// LocalID returns the server's PeerID.
func (s *WebSocketServer) LocalID() PeerID {
	return s.serverID
}

// Peers returns the sorted list of currently connected client PeerIDs.
func (s *WebSocketServer) Peers() []PeerID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PeerID, 0, len(s.clients))
	for id := range s.clients {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// Close stops accepting new clients and closes all active client connections.
func (s *WebSocketServer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	clients := make([]*wsClientConn, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.clients = make(map[PeerID]*wsClientConn)
	s.mu.Unlock()

	for _, c := range clients {
		c.writeMu.Lock()
		_ = writeWSFrameDeadline(c.conn, wsOpClose, false, nil)
		c.writeMu.Unlock()
		_ = c.conn.Close()
	}
	return s.ln.Close()
}
