//go:build !js

package netplay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	stdnet "net"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
)

// WebTransportSupported reports whether WebTransport is available in the current runtime. On native targets it always returns true.
func WebTransportSupported() bool {
	return true
}

func readWTStreamFrame(r io.Reader, limit uint32) ([]byte, error) {
	var hdr [wtStreamHeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > limit {
		return nil, ErrPayloadTooLarge
	}
	return readWTPayload(r, uint64(n))
}

// readWTPayload reads exactly n bytes without eagerly allocating past wsEagerAllocCap so a peer announcing a large stream frame cannot reserve memory it has not sent.
func readWTPayload(r io.Reader, n uint64) ([]byte, error) {
	if n <= wsEagerAllocCap {
		buf := make([]byte, n)
		_, err := io.ReadFull(r, buf)
		return buf, err
	}
	var buf bytes.Buffer
	buf.Grow(wsEagerAllocCap)
	if _, err := io.CopyN(&buf, r, int64(n)); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeWTStreamFrame(str *webtransport.Stream, closing *atomic.Bool, payload []byte) error {
	frame, err := encodeStreamFrame(payload)
	if err != nil {
		return err
	}
	_ = str.SetWriteDeadline(time.Now().Add(wtWriteTimeout))
	if closing.Load() {
		_ = str.SetWriteDeadline(time.Now())
		return ErrClosed
	}
	_, err = str.Write(frame)
	_ = str.SetWriteDeadline(time.Time{})
	return err
}

func wrapDatagramErr(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*quic.DatagramTooLargeError](err); ok {
		return fmt.Errorf("%w: %w", ErrPayloadTooLarge, err)
	}
	return err
}

func buildClientTLSConfig(serverName string, hashes [][]byte) *tls.Config {
	tlsCfg := &tls.Config{
		ServerName: serverName,
		NextProtos: []string{http3.NextProtoH3},
		MinVersion: tls.VersionTLS13,
	}
	if len(hashes) > 0 {
		configurePinnedCertificateVerification(tlsCfg, hashes)
	}
	return tlsCfg
}

// configurePinnedCertificateVerification replaces system CA chain checks with constant-time SHA-256 leaf certificate pinning, matching the W3C WebTransport serverCertificateHashes mechanism. Go's crypto/tls requires InsecureSkipVerify when custom verification replaces CA checks; TLS 1.3 still verifies the server's CertificateVerify signature over the leaf certificate before invoking VerifyPeerCertificate.
func configurePinnedCertificateVerification(tlsCfg *tls.Config, hashes [][]byte) {
	tlsCfg.InsecureSkipVerify = true
	tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return errors.New("net: webtransport server presented no certificate")
		}
		sum := sha256.Sum256(rawCerts[0])
		for _, want := range hashes {
			if subtle.ConstantTimeCompare(sum[:], want) == 1 {
				return nil
			}
		}
		return errors.New("net: webtransport server certificate hash mismatch")
	}
}

type wtDialConfig struct {
	dialAddr func(ctx context.Context, addr string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error)
	headers  http.Header
	idle     time.Duration
	ping     time.Duration
}

// WebTransportTransport is a client-side WebTransport Transport over HTTP/3 and QUIC.
//
// ChannelReliableOrdered packets travel over one bidirectional QUIC stream with 4-byte length-prefixed frames; ChannelUnreliable packets travel as independent QUIC datagrams so a lost packet never blocks newer unreliable updates.
//
// The first message each way on the bidirectional stream is the connection handshake: the client presents its token and the server answers with the ID it assigned, or a reason for refusing. A dropped client keeps its session on the server for Auth.GracePeriod, and Reconnect resumes it with the same ID.
type WebTransportTransport struct {
	rawURL     string
	serverName string
	token      []byte
	hashes     [][]byte
	dialCfg    wtDialConfig

	dialMu   sync.Mutex // serializes Reconnect
	writeMu  sync.Mutex // serializes reliable stream writes
	closing  atomic.Bool
	relSeq   uint32
	unrelSeq atomic.Uint32

	mu       sync.Mutex
	tr       *webtransport.Transport
	sess     *webtransport.Session
	stream   *webtransport.Stream
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

// DialWebTransport opens a WebTransport session to rawURL (https://), presents token in the connection handshake on the reliable stream, and returns a Transport ready for non-blocking Send/Recv once the server accepts it. The server assigns the client's ID, which LocalID reports. A refusal returns a *RejectError that matches ErrRejected.
//
// When opts.ServerCertificateHashes is non-empty, certificate verification matches the server's leaf certificate SHA-256 digest against the provided hashes instead of system root CAs.
func DialWebTransport(rawURL string, token []byte, opts WebTransportOptions) (*WebTransportTransport, error) {
	return dialWebTransport(rawURL, token, opts, wtDialConfig{idle: wtClientIdle, ping: wtClientPing})
}

func dialWebTransport(rawURL string, token []byte, opts WebTransportOptions, cfg wtDialConfig) (*WebTransportTransport, error) {
	if len(token) > MaxTokenSize {
		return nil, ErrTokenTooLarge
	}
	u, err := validateWebTransportURL(rawURL)
	if err != nil {
		return nil, err
	}
	hashes, err := validateCertificateHashes(opts.ServerCertificateHashes)
	if err != nil {
		return nil, err
	}
	if cfg.idle <= 0 {
		cfg.idle = wtClientIdle
	}
	if cfg.ping <= 0 {
		cfg.ping = wtClientPing
	}
	wt := &WebTransportTransport{
		rawURL:     rawURL,
		serverName: u.Hostname(),
		token:      slices.Clone(token),
		hashes:     hashes,
		dialCfg:    cfg,
		changed:    make(chan struct{}, 1),
	}
	if err := wt.connect(); err != nil {
		return nil, err
	}
	return wt, nil
}

// DialWebTransportWithFallback dials rawURL over WebTransport and falls back to DialWebSocket(wsURL, token) when WebTransport cannot connect. Handshake rejections (ErrRejected) and ErrTokenTooLarge are returned directly without falling back.
func DialWebTransportWithFallback(wtURL, wsURL string, token []byte, opts WebTransportOptions) (Transport, error) {
	wt, err := DialWebTransport(wtURL, token, opts)
	if err == nil {
		return wt, nil
	}
	if errors.Is(err, ErrRejected) || errors.Is(err, ErrTokenTooLarge) || wsURL == "" {
		return nil, err
	}
	return DialWebSocket(wsURL, token)
}

// Reconnect drops the current WebTransport session without a goodbye (sending wtCloseReconnect so the server keeps the session for Auth.GracePeriod), dials the server again, and resumes the session: within the server's grace period the client keeps its ID, and Resumed reports true. Past it, the server admits the client as a new peer when its token is still valid, and LocalID changes.
func (wt *WebTransportTransport) Reconnect() error {
	wt.dialMu.Lock()
	defer wt.dialMu.Unlock()
	wt.mu.Lock()
	if wt.state == ConnClosed {
		wt.mu.Unlock()
		return ErrClosed
	}
	oldSess, oldTr := wt.sess, wt.tr
	wt.sess, wt.stream, wt.tr = nil, nil, nil
	wt.state = ConnConnecting
	wt.mu.Unlock()
	if oldSess != nil {
		_ = oldSess.CloseWithError(wtCloseReconnect, "reconnect")
	}
	if oldTr != nil {
		_ = oldTr.Close()
	}
	return wt.connect()
}

func (wt *WebTransportTransport) connect() error {
	wt.mu.Lock()
	if wt.state == ConnClosed {
		wt.mu.Unlock()
		return ErrClosed
	}
	res := wt.resume
	wt.mu.Unlock()

	tr := &webtransport.Transport{
		TLSClientConfig: buildClientTLSConfig(wt.serverName, wt.hashes),
		QUICConfig: &quic.Config{
			EnableDatagrams:                  true,
			EnableStreamResetPartialDelivery: true,
			MaxIdleTimeout:                   wt.dialCfg.idle,
			KeepAlivePeriod:                  wt.dialCfg.ping,
		},
		DialAddr: wt.dialCfg.dialAddr,
	}

	ctx, cancel := context.WithTimeout(context.Background(), wtHandshakeTimeout)
	defer cancel()

	rsp, sess, err := tr.Dial(ctx, wt.rawURL, wt.dialCfg.headers)
	if err != nil {
		_ = tr.Close()
		if rsp != nil && rsp.StatusCode != http.StatusOK {
			return wt.fail(fmt.Errorf("net: webtransport upgrade rejected with status %d: %w", rsp.StatusCode, err))
		}
		return wt.fail(err)
	}

	stream, err := sess.OpenStreamSync(ctx)
	if err != nil {
		_ = sess.CloseWithError(wtCloseReconnect, "open stream failed")
		_ = tr.Close()
		return wt.fail(err)
	}

	w, err := wtClientHandshake(stream, wt.token, res)
	if err != nil {
		if errors.Is(err, ErrRejected) {
			_ = sess.CloseWithError(wtCloseGoodbye, "rejected")
		} else {
			_ = sess.CloseWithError(wtCloseReconnect, "handshake failed")
		}
		_ = tr.Close()
		return wt.fail(err)
	}
	_ = stream.SetDeadline(time.Time{})

	wt.mu.Lock()
	if wt.state == ConnClosed {
		wt.mu.Unlock()
		_ = sess.CloseWithError(wtCloseGoodbye, "closed")
		_ = tr.Close()
		return ErrClosed
	}
	wt.tr, wt.sess, wt.stream = tr, sess, stream
	wt.localID, wt.serverID = w.peer, w.server
	wt.resume = resumeState{peer: w.peer, secret: w.secret}
	wt.resumed = w.resumed
	if !w.resumed {
		wt.admissions++
	}
	wt.state = ConnOpen
	wt.err = nil
	wt.mu.Unlock()

	go wt.readDatagramLoop(sess)
	go wt.readReliableLoop(sess, stream)
	return nil
}

func wtClientHandshake(stream *webtransport.Stream, token []byte, res resumeState) (welcome, error) {
	_ = stream.SetDeadline(time.Now().Add(wtHandshakeTimeout))
	h, err := newHello(token, res)
	if err != nil {
		return welcome{}, err
	}
	frame, err := encodeStreamFrame(encodeHello(h, 0))
	if err != nil {
		return welcome{}, err
	}
	if _, err := stream.Write(frame); err != nil {
		return welcome{}, err
	}
	reply, err := readWTStreamFrame(stream, wtMaxReplyFrame)
	if err != nil {
		return welcome{}, fmt.Errorf("net: webtransport handshake read: %w", err)
	}
	return acceptServerHandshake(reply, h.nonce)
}

func (wt *WebTransportTransport) fail(err error) error {
	wt.mu.Lock()
	if wt.state != ConnClosed {
		wt.state = ConnDisconnected
		wt.err = err
	}
	wt.mu.Unlock()
	return err
}

func (wt *WebTransportTransport) readReliableLoop(sess *webtransport.Session, stream *webtransport.Stream) {
	cause := ErrDisconnected
	defer func() {
		wt.mu.Lock()
		if wt.sess == sess {
			wt.sess, wt.stream = nil, nil
			if wt.state == ConnOpen {
				wt.state = ConnDisconnected
				wt.err = cause
			}
		}
		wt.mu.Unlock()
		_ = sess.CloseWithError(wtCloseReconnect, "")
		wt.notify()
	}()

	for {
		frame, err := readWTStreamFrame(stream, wtMaxStreamMessageSize)
		if err != nil {
			cause = fmt.Errorf("%w: %w", ErrDisconnected, err)
			return
		}
		ch, seq, payload, err := decodeWireFrame(frame)
		if err != nil || ch != ChannelReliableOrdered {
			continue
		}
		wt.deliver(sess, ch, seq, payload)
	}
}

func (wt *WebTransportTransport) readDatagramLoop(sess *webtransport.Session) {
	ctx := sess.Context()
	for {
		dgram, err := sess.ReceiveDatagram(ctx)
		if err != nil {
			return
		}
		ch, seq, payload, err := decodeWireFrame(dgram)
		if err != nil || ch != ChannelUnreliable {
			continue
		}
		wt.deliver(sess, ch, seq, payload)
	}
}

func (wt *WebTransportTransport) deliver(sess *webtransport.Session, ch Channel, seq uint32, payload []byte) {
	cp := slices.Clone(payload)
	wt.mu.Lock()
	if wt.sess == sess && wt.state == ConnOpen {
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

func (wt *WebTransportTransport) notify() {
	select {
	case wt.changed <- struct{}{}:
	default:
	}
}

// Send transmits payload to the server. ChannelReliableOrdered frames travel on the bidirectional QUIC stream; ChannelUnreliable frames travel as independent QUIC datagrams (returning ErrPayloadTooLarge if payload exceeds the QUIC datagram MTU).
func (wt *WebTransportTransport) Send(to PeerID, ch Channel, payload []byte) error {
	wt.mu.Lock()
	state, sess, stream, serverID := wt.state, wt.sess, wt.stream, wt.serverID
	wt.mu.Unlock()
	switch {
	case state == ConnClosed:
		return ErrClosed
	case state != ConnOpen || sess == nil || stream == nil:
		return ErrDisconnected
	}
	if to != PeerBroadcast && to != serverID {
		return ErrUnknownPeer
	}
	switch ch {
	case ChannelReliableOrdered:
		wt.writeMu.Lock()
		defer wt.writeMu.Unlock()
		wt.relSeq++
		frame := encodeWireFrame(ch, wt.relSeq, payload)
		return writeWTStreamFrame(stream, &wt.closing, frame)
	case ChannelUnreliable:
		seq := wt.unrelSeq.Add(1)
		frame := encodeWireFrame(ch, seq, payload)
		return wrapDatagramErr(sess.SendDatagram(frame))
	default:
		return ErrInvalidPacket
	}
}

// SetInboxLimits bounds the packets the transport keeps until Recv takes them (see InboxLimits).
func (wt *WebTransportTransport) SetInboxLimits(l InboxLimits) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	wt.inbox.setLimits(l)
}

// InboxStats reports what the transport's inbox holds and what its limits dropped or warned about.
func (wt *WebTransportTransport) InboxStats() InboxStats {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.inbox.stats()
}

// Recv pops the next received packet without blocking.
func (wt *WebTransportTransport) Recv() (Packet, bool) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.inbox.pop()
}

// LocalID returns the PeerID the server assigned this client.
func (wt *WebTransportTransport) LocalID() PeerID {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.localID
}

// Peers returns the connected server's PeerID, or nil while disconnected or closed.
func (wt *WebTransportTransport) Peers() []PeerID {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if wt.state != ConnOpen {
		return nil
	}
	return []PeerID{wt.serverID}
}

// State returns the connection state.
func (wt *WebTransportTransport) State() ConnState {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.state
}

// Err returns why the client is disconnected: a *RejectError, or ErrDisconnected wrapping the I/O error. It is nil while the connection is open.
func (wt *WebTransportTransport) Err() error {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.err
}

// Resumed reports whether the last handshake resumed an existing session, keeping the client's ID.
func (wt *WebTransportTransport) Resumed() bool {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.resumed
}

// relayAdmissions counts the handshakes that admitted the client as a new peer.
func (wt *WebTransportTransport) relayAdmissions() uint64 {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.admissions
}

// Close sends a clean goodbye on the reliable stream and closes the WebTransport session so the server reports PeerLeft at once rather than holding the session for a reconnect.
func (wt *WebTransportTransport) Close() error {
	wt.mu.Lock()
	if wt.state == ConnClosed {
		wt.mu.Unlock()
		return nil
	}
	wt.state = ConnClosed
	sess, stream, tr := wt.sess, wt.stream, wt.tr
	wt.sess, wt.stream, wt.tr = nil, nil, nil
	wt.mu.Unlock()

	wt.closing.Store(true)
	if stream != nil {
		// Break any blocked write, then send a 0-length goodbye frame and FIN on the reliable stream before closing the QUIC connection so CONNECTION_CLOSE cannot race ahead of it.
		_ = stream.SetWriteDeadline(time.Now())
		wt.writeMu.Lock()
		_ = stream.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
		_, _ = stream.Write([]byte{0, 0, 0, 0})
		_ = stream.Close()
		wt.writeMu.Unlock()
	}
	if sess != nil {
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-sess.Context().Done():
		case <-timer.C:
		}
		timer.Stop()
	}
	var err error
	if sess != nil {
		err = sess.CloseWithError(wtCloseGoodbye, "goodbye")
	}
	if tr != nil {
		_ = tr.Close()
	}
	return err
}

type wtClientConn struct {
	id       PeerID
	sess     *webtransport.Session
	stream   *webtransport.Stream
	writeMu  sync.Mutex
	closing  atomic.Bool
	relSeq   uint32
	unrelSeq atomic.Uint32
}

// WebTransportServer accepts WebTransport clients over HTTP/3 and QUIC and exposes them through the standard Transport and RelayServer interfaces.
//
// ChannelReliableOrdered packets travel over each client's bidirectional QUIC stream, and ChannelUnreliable packets travel as independent QUIC datagrams. Every client runs the connection handshake on the bidirectional stream before its packets reach Recv: Auth.Verify checks its token, the server assigns its ID and tells the client, and NextEvent reports joins, drops, resumes and leaves.
type WebTransportServer struct {
	pc        stdnet.PacketConn
	wtSrv     *webtransport.Server
	serverID  PeerID
	origins   []string
	serveDone chan struct{}
	closeCh   chan struct{}

	mu      sync.Mutex
	table   *sessionTable
	inbox   packetQueue
	closed  bool
	changed chan struct{}
}

// ListenWebTransport starts a WebTransport server over QUIC/HTTP/3 on addr. serverID is the server's own PeerID (1 when zero); clients get the IDs after it. auth decides who may connect and how long a dropped client keeps its ID. cfg provides the TLS 1.3 certificate and is required. allowedOrigins lists the browser Origins allowed to connect; empty allows any, "*" allows all, and an Origin outside the list is refused with 403 before the handshake.
func ListenWebTransport(addr string, serverID PeerID, auth Auth, cfg *tls.Config, allowedOrigins []string) (*WebTransportServer, error) {
	if cfg == nil {
		return nil, errors.New("net: webtransport needs a tls.Config")
	}
	origins, err := normalizeOrigins(allowedOrigins)
	if err != nil {
		return nil, err
	}
	pc, err := stdnet.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	srv, err := listenWebTransportConn(pc, serverID, auth, cfg, origins)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	return srv, nil
}

// ListenWebTransportFiles is ListenWebTransport with a certificate and key loaded from PEM files.
func ListenWebTransportFiles(addr string, serverID PeerID, auth Auth, certFile, keyFile string, allowedOrigins []string) (*WebTransportServer, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return ListenWebTransport(addr, serverID, auth, &tls.Config{Certificates: []tls.Certificate{cert}}, allowedOrigins)
}

func listenWebTransportConn(pc stdnet.PacketConn, serverID PeerID, auth Auth, cfg *tls.Config, origins []string) (*WebTransportServer, error) {
	if serverID == 0 {
		serverID = 1
	}
	tlsCfg := cfg.Clone()
	if tlsCfg.MinVersion < tls.VersionTLS13 {
		tlsCfg.MinVersion = tls.VersionTLS13
	}
	if !slices.Contains(tlsCfg.NextProtos, http3.NextProtoH3) {
		tlsCfg.NextProtos = append([]string{http3.NextProtoH3}, tlsCfg.NextProtos...)
	}

	ka := serverKeepAlive(auth)
	s := &WebTransportServer{
		pc:        pc,
		serverID:  serverID,
		origins:   origins,
		serveDone: make(chan struct{}),
		closeCh:   make(chan struct{}),
		table:     newSessionTable(auth, serverID, false),
		changed:   make(chan struct{}, 1),
	}

	h3Srv := &http3.Server{
		TLSConfig: tlsCfg,
		QUICConfig: &quic.Config{
			MaxIdleTimeout:                   ka.idle,
			KeepAlivePeriod:                  ka.ping,
			EnableDatagrams:                  true,
			EnableStreamResetPartialDelivery: true,
		},
		EnableDatagrams: true,
	}
	wtSrv := &webtransport.Server{
		H3: h3Srv,
		CheckOrigin: func(r *http.Request) bool {
			return originAllowed(s.origins, r.Header.Get("Origin"))
		},
	}
	h3Srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !originAllowed(s.origins, r.Header.Get("Origin")) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		sess, err := wtSrv.Upgrade(w, r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		go s.handleSession(sess)
	})
	s.wtSrv = wtSrv

	go func() {
		defer close(s.serveDone)
		_ = wtSrv.Serve(pc)
	}()
	return s, nil
}

// Addr returns the listener's UDP network address.
func (s *WebTransportServer) Addr() stdnet.Addr {
	return s.pc.LocalAddr()
}

func (s *WebTransportServer) handleSession(sess *webtransport.Session) {
	hsCtx, cancel := context.WithTimeout(sess.Context(), wtHandshakeTimeout)
	stream, err := sess.AcceptStream(hsCtx)
	cancel()
	if err != nil {
		_ = sess.CloseWithError(0, "missing stream")
		return
	}

	_ = stream.SetDeadline(time.Now().Add(wtHandshakeTimeout))
	payload, err := readWTStreamFrame(stream, wtMaxHelloFrame)
	if err != nil {
		_ = sess.CloseWithError(0, "handshake read failed")
		return
	}
	h, err := decodeHello(payload)
	if err != nil {
		if errors.Is(err, errHandshakeVersion) {
			if frame, ferr := encodeStreamFrame(encodeReject(h.nonce, hsReasonVersion, 0)); ferr == nil {
				_, _ = stream.Write(frame)
			}
			_ = stream.Close()
			s.waitRejectDrain(sess)
		}
		_ = sess.CloseWithError(0, "invalid hello")
		return
	}

	client := &wtClientConn{sess: sess, stream: stream}
	// Hold the client's write lock until the welcome is out, so no game packet overtakes it once the session is visible to Send.
	client.writeMu.Lock()
	reply, ok := s.admit(h, client)
	if reply != nil {
		if frame, ferr := encodeStreamFrame(reply); ferr == nil {
			_, err = stream.Write(frame)
		} else {
			err = ferr
		}
	}
	if !ok {
		_ = stream.Close()
		client.writeMu.Unlock()
		s.waitRejectDrain(sess)
		_ = sess.CloseWithError(0, "rejected")
		return
	}
	client.writeMu.Unlock()
	if err != nil {
		s.clientGone(client, false)
		return
	}
	_ = stream.SetDeadline(time.Time{})

	dgramDone := make(chan struct{})
	go func() {
		defer close(dgramDone)
		s.readDatagramLoop(client)
	}()
	s.readReliableLoop(client)
	<-dgramDone
}

// waitRejectDrain gives the client a short window to read the rejection frame from the closed reliable stream before CloseWithError cancels the session.
func (s *WebTransportServer) waitRejectDrain(sess *webtransport.Session) {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-sess.Context().Done():
	case <-s.closeCh:
	case <-timer.C:
	}
}

func (s *WebTransportServer) admit(h hello, c *wtClientConn) ([]byte, bool) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, false
	}
	now := s.table.now()
	s.table.expire(now)
	if sess := s.table.resume(h, now); sess != nil {
		old, _ := sess.link.(*wtClientConn)
		sess.link = c
		c.id = sess.id
		reply := encodeWelcome(s.table.welcomeFor(sess, h.nonce, true))
		s.mu.Unlock()
		if old != nil {
			_ = old.sess.CloseWithError(0, "replaced")
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

func (s *WebTransportServer) clientGone(c *wtClientConn, goodbye bool) {
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
	_ = c.sess.CloseWithError(0, "")
	s.notify()
}

func (s *WebTransportServer) readReliableLoop(c *wtClientConn) {
	goodbye := false
	defer func() { s.clientGone(c, goodbye) }()

	for {
		frame, err := readWTStreamFrame(c.stream, wtMaxStreamMessageSize)
		if err != nil {
			if errors.Is(err, io.EOF) {
				goodbye = true
			} else if sessErr, ok := errors.AsType[*webtransport.SessionError](err); ok && sessErr.Remote && sessErr.ErrorCode != wtCloseReconnect {
				goodbye = true
			}
			return
		}
		if len(frame) == 0 {
			// Explicit 0-length goodbye frame sent by Close().
			goodbye = true
			return
		}
		ch, seq, payload, err := decodeWireFrame(frame)
		if err != nil || ch != ChannelReliableOrdered {
			continue
		}
		if !s.deliverFromClient(c, ch, seq, payload) {
			goodbye = true // past its reliable inbox cap: end the session so its reliable stream never resumes with a gap
			return
		}
	}
}

func (s *WebTransportServer) readDatagramLoop(c *wtClientConn) {
	ctx := c.sess.Context()
	for {
		dgram, err := c.sess.ReceiveDatagram(ctx)
		if err != nil {
			return
		}
		ch, seq, payload, err := decodeWireFrame(dgram)
		if err != nil || ch != ChannelUnreliable {
			continue
		}
		s.deliverFromClient(c, ch, seq, payload)
	}
}

func (s *WebTransportServer) deliverFromClient(c *wtClientConn, ch Channel, seq uint32, payload []byte) bool {
	cp := slices.Clone(payload)
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

func (s *WebTransportServer) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// Send transmits payload to a connected client (or all clients when to == PeerBroadcast). ChannelReliableOrdered frames travel on each client's bidirectional stream; ChannelUnreliable frames travel as independent QUIC datagrams (returning ErrPayloadTooLarge if payload exceeds the QUIC datagram MTU). It returns ErrDisconnected for a client inside its grace period and ErrUnknownPeer for an ID with no session.
func (s *WebTransportServer) Send(to PeerID, ch Channel, payload []byte) error {
	if ch != ChannelReliableOrdered && ch != ChannelUnreliable {
		return ErrInvalidPacket
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	var targets []*wtClientConn
	if to == PeerBroadcast {
		for _, id := range s.table.connectedIDs() {
			targets = append(targets, s.table.sessions[id].link.(*wtClientConn))
		}
	} else {
		if err := s.table.sendable(to); err != nil {
			s.mu.Unlock()
			return err
		}
		targets = []*wtClientConn{s.table.sessions[to].link.(*wtClientConn)}
	}
	s.mu.Unlock()

	var firstErr error
	for _, c := range targets {
		var err error
		if ch == ChannelReliableOrdered {
			c.writeMu.Lock()
			c.relSeq++
			frame := encodeWireFrame(ch, c.relSeq, payload)
			err = writeWTStreamFrame(c.stream, &c.closing, frame)
			c.writeMu.Unlock()
		} else {
			seq := c.unrelSeq.Add(1)
			frame := encodeWireFrame(ch, seq, payload)
			err = wrapDatagramErr(c.sess.SendDatagram(frame))
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SetInboxLimits bounds the packets the server keeps until Recv takes them (see InboxLimits). A client whose queued reliable packets would pass InboxLimits.Reliable has its session ended, as if it had left.
func (s *WebTransportServer) SetInboxLimits(l InboxLimits) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inbox.setLimits(l)
}

// InboxStats reports what the server's inbox holds and what its limits dropped or warned about.
func (s *WebTransportServer) InboxStats() InboxStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inbox.stats()
}

// Recv pops the next received packet from any connected client without blocking.
func (s *WebTransportServer) Recv() (Packet, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inbox.pop()
}

// NextEvent pops the next join, drop, resume or leave without blocking, after ending the sessions whose grace period has passed.
func (s *WebTransportServer) NextEvent() (PeerEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.table.expire(s.table.now())
	return s.table.popEvent()
}

// LocalID returns the server's PeerID.
func (s *WebTransportServer) LocalID() PeerID {
	return s.serverID
}

// Peers returns the sorted list of currently connected client PeerIDs. Clients inside their grace period are left out.
func (s *WebTransportServer) Peers() []PeerID {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.table.expire(s.table.now())
	return s.table.connectedIDs()
}

// Close stops accepting new clients and closes all active client sessions and the underlying UDP socket.
func (s *WebTransportServer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.closeCh)
	var clients []*wtClientConn
	for _, sess := range s.table.sessions {
		if c, ok := sess.link.(*wtClientConn); ok {
			clients = append(clients, c)
		}
	}
	s.mu.Unlock()

	for _, c := range clients {
		c.closing.Store(true)
		_ = c.stream.SetWriteDeadline(time.Now())
		_ = c.sess.CloseWithError(wtCloseGoodbye, "server closed")
	}

	err := s.wtSrv.Close()
	pcErr := s.pc.Close()
	<-s.serveDone
	if err != nil {
		return err
	}
	return pcErr
}

// relayWake lets a Relay serving this server wait for packets and events instead of polling.
func (s *WebTransportServer) relayWake() <-chan struct{} {
	return s.changed
}

var (
	_ Transport     = (*WebTransportTransport)(nil)
	_ InboxReporter = (*WebTransportTransport)(nil)
	_ relayAdmitted = (*WebTransportTransport)(nil)
	_ Transport     = (*WebTransportServer)(nil)
	_ PeerEvents    = (*WebTransportServer)(nil)
	_ RelayServer   = (*WebTransportServer)(nil)
	_ InboxReporter = (*WebTransportServer)(nil)
	_ relayWaker    = (*WebTransportServer)(nil)
)
