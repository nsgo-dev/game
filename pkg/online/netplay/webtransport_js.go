//go:build js && wasm

package netplay

import (
	"errors"
	"slices"
	"sync"
	"syscall/js"
)

// WebTransportSupported reports whether the browser exposes the WebTransport API.
func WebTransportSupported() bool {
	return js.Global().Get("WebTransport").Truthy()
}

type wtPendingFrame struct {
	ch    Channel
	frame []byte
}

// WebTransportTransport wraps the browser WebAssembly WebTransport API.
//
// ChannelReliableOrdered packets travel over one bidirectional QUIC stream with 4-byte length-prefixed frames; ChannelUnreliable packets travel as independent QUIC datagrams.
//
// The browser connects asynchronously: DialWebTransport returns at once, sends the connection handshake on the reliable stream once the session is ready, and the transport becomes ConnOpen when the server's welcome arrives. Until then LocalID is 0, and Send queues packets to flush after the welcome. A refusal moves it to ConnDisconnected with a *RejectError in Err.
type WebTransportTransport struct {
	rawURL string
	token  []byte
	hashes [][]byte

	mu        sync.Mutex
	attempt   uint64
	jsWT      js.Value
	localID   PeerID
	serverID  PeerID
	state     ConnState
	err       error
	resume    resumeState
	resumed   bool
	maxDgram  int
	seq       [2]uint32
	inbox     packetQueue
	pending   []wtPendingFrame
	relOut    [][]byte
	relNotify chan struct{}
	dgramOut  chan []byte
	done      chan struct{}

	// admissions counts handshakes that admitted the client as a new peer rather than resuming, so a RelayTransport can tell its relay session was lost even when the new ID equals the old one.
	admissions uint64
}

// DialWebTransport opens a browser WebTransport session to rawURL (https://) that presents token in the connection handshake on the reliable stream, and returns a Transport ready for non-blocking Send/Recv. The server assigns the client's ID, which LocalID reports once State is ConnOpen.
//
// When opts.ServerCertificateHashes is non-empty, each 32-byte SHA-256 hash is passed via serverCertificateHashes so self-signed development certificates work without installing a local CA.
func DialWebTransport(rawURL string, token []byte, opts WebTransportOptions) (*WebTransportTransport, error) {
	if len(token) > MaxTokenSize {
		return nil, ErrTokenTooLarge
	}
	if _, err := validateWebTransportURL(rawURL); err != nil {
		return nil, err
	}
	hashes, err := validateCertificateHashes(opts.ServerCertificateHashes)
	if err != nil {
		return nil, err
	}
	if !WebTransportSupported() {
		return nil, ErrWebTransportUnavailable
	}
	wt := &WebTransportTransport{
		rawURL: rawURL,
		token:  slices.Clone(token),
		hashes: hashes,
		jsWT:   js.Null(),
	}
	wt.mu.Lock()
	err = wt.openLocked()
	wt.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return wt, nil
}

// DialWebTransportWithFallback dials wtURL over WebTransport when the browser exposes the WebTransport API, and otherwise falls back immediately to DialWebSocket(wsURL, token).
func DialWebTransportWithFallback(wtURL, wsURL string, token []byte, opts WebTransportOptions) (Transport, error) {
	if !WebTransportSupported() {
		if wsURL == "" {
			return nil, ErrWebTransportUnavailable
		}
		return DialWebSocket(wsURL, token)
	}
	wt, err := DialWebTransport(wtURL, token, opts)
	if err == nil {
		return wt, nil
	}
	if errors.Is(err, ErrTokenTooLarge) || wsURL == "" {
		return nil, err
	}
	return DialWebSocket(wsURL, token)
}

// Reconnect closes the current WebTransport session, if any, with wtCloseReconnect (so the server keeps the session for Auth.GracePeriod) and opens a new one that resumes the session: within the grace period the client keeps its ID, and Resumed reports true once State is ConnOpen again. Past it, the server admits the client as a new peer when its token is still valid, and LocalID changes.
func (wt *WebTransportTransport) Reconnect() error {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if wt.state == ConnClosed {
		return ErrClosed
	}
	wt.releaseLocked(true)
	return wt.openLocked()
}

func (wt *WebTransportTransport) openLocked() error {
	h, err := newHello(wt.token, wt.resume)
	if err != nil {
		return err
	}
	jsWT, err := constructJSWebTransport(wt.rawURL, wt.hashes)
	if err != nil {
		return err
	}
	wt.attempt++
	attempt := wt.attempt
	done := make(chan struct{})
	wt.jsWT = jsWT
	wt.state = ConnConnecting
	wt.err = nil
	wt.maxDgram = 0
	wt.relOut = nil
	wt.relNotify = make(chan struct{}, 1)
	wt.dgramOut = make(chan []byte, 256)
	wt.done = done

	go wt.runAttempt(attempt, jsWT, h, wt.relNotify, wt.dgramOut, done)
	return nil
}

func constructJSWebTransport(rawURL string, hashes [][]byte) (val js.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("net: failed to construct browser WebTransport")
		}
	}()
	ctor := js.Global().Get("WebTransport")
	if len(hashes) == 0 {
		return ctor.New(rawURL), nil
	}
	jsHashes := js.Global().Get("Array").New(len(hashes))
	for i, h := range hashes {
		u8 := bytesToJS(h)
		entry := js.Global().Get("Object").New()
		entry.Set("algorithm", "sha-256")
		entry.Set("value", u8)
		jsHashes.SetIndex(i, entry)
	}
	jsOpts := js.Global().Get("Object").New()
	jsOpts.Set("serverCertificateHashes", jsHashes)
	return ctor.New(rawURL, jsOpts), nil
}

func (wt *WebTransportTransport) runAttempt(attempt uint64, jsWT js.Value, h hello, relNotify chan struct{}, dgramOut chan []byte, done chan struct{}) {
	// Always consume wt.closed so browsers never report an unhandled promise rejection.
	go func() {
		_, err := awaitPromise(jsWT.Get("closed"))
		if err == nil {
			err = ErrDisconnected
		}
		wt.markDisconnected(attempt, err)
	}()

	if _, err := awaitPromise(jsWT.Get("ready")); err != nil {
		wt.markDisconnected(attempt, err)
		return
	}
	if !wt.isCurrent(attempt) {
		return
	}

	bidi, err := awaitPromise(jsWT.Call("createBidirectionalStream"))
	if err != nil {
		wt.markDisconnected(attempt, err)
		closeJSWT(jsWT, true)
		return
	}
	relReader := bidi.Get("readable").Call("getReader")
	relWriter := bidi.Get("writable").Call("getWriter")

	helloFrame, err := encodeStreamFrame(encodeHello(h, 0))
	if err != nil {
		wt.markDisconnected(attempt, err)
		closeJSWT(jsWT, true)
		return
	}
	if _, err := awaitPromise(relWriter.Call("write", bytesToJS(helloFrame))); err != nil {
		wt.markDisconnected(attempt, err)
		closeJSWT(jsWT, true)
		return
	}

	var dec wtStreamDecoder
	reply, err := readNextJSFrame(relReader, &dec, wtMaxReplyFrame)
	if err != nil {
		wt.markDisconnected(attempt, err)
		closeJSWT(jsWT, true)
		return
	}

	w, err := acceptServerHandshake(reply, h.nonce)
	if err != nil {
		wt.markDisconnected(attempt, err)
		// A garbled welcome may still have admitted the client, so leave the session for Reconnect to resume.
		closeJSWT(jsWT, !errors.Is(err, ErrRejected))
		return
	}

	datagrams := jsWT.Get("datagrams")
	maxDgram := 0
	if ms := datagrams.Get("maxDatagramSize"); ms.Type() == js.TypeNumber {
		maxDgram = ms.Int()
	}
	dgramReader := datagrams.Get("readable").Call("getReader")
	dgramWriter := datagrams.Get("writable").Call("getWriter")

	wt.mu.Lock()
	if wt.attempt != attempt || wt.state == ConnClosed {
		wt.mu.Unlock()
		closeJSWT(jsWT, false)
		return
	}
	wt.localID, wt.serverID = w.peer, w.server
	wt.resume = resumeState{peer: w.peer, secret: w.secret}
	wt.resumed = w.resumed
	if !w.resumed {
		wt.admissions++
	}
	wt.maxDgram = maxDgram
	wt.state = ConnOpen
	queued := wt.pending
	wt.pending = nil
	for _, pf := range queued {
		if pf.ch == ChannelReliableOrdered {
			if framed, ferr := encodeStreamFrame(pf.frame); ferr == nil {
				wt.relOut = append(wt.relOut, framed)
			}
		} else {
			if maxDgram <= 0 || len(pf.frame) <= maxDgram {
				select {
				case dgramOut <- pf.frame:
				default:
				}
			}
		}
	}
	if len(wt.relOut) > 0 {
		select {
		case relNotify <- struct{}{}:
		default:
		}
	}
	wt.mu.Unlock()

	go wt.reliableWriteLoop(attempt, relWriter, relNotify, done)
	go wt.datagramWriteLoop(dgramWriter, dgramOut, done)
	go wt.datagramReadLoop(attempt, dgramReader)
	wt.reliableReadLoop(attempt, &dec, relReader)
}

func (wt *WebTransportTransport) reliableWriteLoop(attempt uint64, writer js.Value, notify <-chan struct{}, done <-chan struct{}) {
	defer func() {
		releaseLockSafe(writer)
	}()
	for {
		wt.mu.Lock()
		if wt.attempt != attempt || wt.state != ConnOpen {
			wt.mu.Unlock()
			return
		}
		var batch [][]byte
		if len(wt.relOut) > 0 {
			batch = wt.relOut
			wt.relOut = nil
		}
		wt.mu.Unlock()

		for _, framed := range batch {
			if _, err := awaitPromise(writer.Call("write", bytesToJS(framed))); err != nil {
				wt.markDisconnected(attempt, err)
				return
			}
		}

		select {
		case <-done:
			return
		case <-notify:
		}
	}
}

func (wt *WebTransportTransport) datagramWriteLoop(writer js.Value, dgramOut <-chan []byte, done <-chan struct{}) {
	defer func() {
		releaseLockSafe(writer)
	}()
	for {
		select {
		case <-done:
			return
		case frame := <-dgramOut:
			if _, err := awaitPromise(writer.Call("write", bytesToJS(frame))); err != nil {
				return
			}
		}
	}
}

func (wt *WebTransportTransport) reliableReadLoop(attempt uint64, dec *wtStreamDecoder, reader js.Value) {
	defer func() {
		releaseLockSafe(reader)
		wt.markDisconnected(attempt, ErrDisconnected)
	}()
	for {
		frame, err := readNextJSFrame(reader, dec, wtMaxStreamMessageSize)
		if err != nil {
			return
		}
		ch, seq, payload, err := decodeWireFrame(frame)
		if err != nil || ch != ChannelReliableOrdered {
			continue
		}
		wt.deliver(attempt, ch, seq, payload)
	}
}

func (wt *WebTransportTransport) datagramReadLoop(attempt uint64, reader js.Value) {
	defer func() {
		releaseLockSafe(reader)
	}()
	for {
		res, err := awaitPromise(reader.Call("read"))
		if err != nil || !res.Truthy() || res.Get("done").Bool() {
			return
		}
		val := res.Get("value")
		if !val.Truthy() {
			continue
		}
		dgram := jsToBytes(val)
		ch, seq, payload, err := decodeWireFrame(dgram)
		if err != nil || ch != ChannelUnreliable {
			continue
		}
		wt.deliver(attempt, ch, seq, payload)
	}
}

func (wt *WebTransportTransport) deliver(attempt uint64, ch Channel, seq uint32, payload []byte) {
	cp := slices.Clone(payload)
	wt.mu.Lock()
	if wt.attempt == attempt && wt.state == ConnOpen {
		wt.inbox.push(Packet{
			From:    wt.serverID,
			Channel: ch,
			Payload: cp,
			Seq:     seq,
		})
	}
	wt.mu.Unlock()
}

func (wt *WebTransportTransport) isCurrent(attempt uint64) bool {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.attempt == attempt && wt.state != ConnClosed
}

func (wt *WebTransportTransport) markDisconnected(attempt uint64, cause error) {
	wt.mu.Lock()
	if wt.attempt == attempt && wt.state != ConnClosed {
		if wt.state == ConnOpen || wt.err == nil {
			if cause == nil {
				cause = ErrDisconnected
			}
			wt.err = cause
		}
		wt.state = ConnDisconnected
		if wt.done != nil {
			close(wt.done)
			wt.done = nil
		}
	}
	wt.mu.Unlock()
}

func (wt *WebTransportTransport) releaseLocked(reconnect bool) {
	wt.attempt++
	if wt.done != nil {
		close(wt.done)
		wt.done = nil
	}
	wt.relOut = nil
	wt.pending = nil
	if !wt.jsWT.IsNull() && !wt.jsWT.IsUndefined() {
		closeJSWT(wt.jsWT, reconnect)
		wt.jsWT = js.Null()
	}
}

func closeJSWT(jsWT js.Value, reconnect bool) {
	defer func() { _ = recover() }()
	if reconnect {
		info := js.Global().Get("Object").New()
		info.Set("closeCode", wtCloseReconnect)
		jsWT.Call("close", info)
		return
	}
	jsWT.Call("close")
}

func releaseLockSafe(v js.Value) {
	defer func() { _ = recover() }()
	if v.Truthy() {
		v.Call("releaseLock")
	}
}

func readNextJSFrame(reader js.Value, dec *wtStreamDecoder, limit uint32) ([]byte, error) {
	for {
		frame, ok, err := dec.next(limit)
		if err != nil {
			return nil, err
		}
		if ok {
			return frame, nil
		}
		res, err := awaitPromise(reader.Call("read"))
		if err != nil {
			return nil, err
		}
		if !res.Truthy() || res.Get("done").Bool() {
			return nil, ErrDisconnected
		}
		val := res.Get("value")
		if val.Truthy() {
			dec.write(jsToBytes(val))
		}
	}
}

func bytesToJS(b []byte) js.Value {
	u8 := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(u8, b)
	return u8
}

func jsToBytes(val js.Value) []byte {
	u8 := val
	if !val.InstanceOf(js.Global().Get("Uint8Array")) {
		u8 = js.Global().Get("Uint8Array").New(val)
	}
	buf := make([]byte, u8.Get("byteLength").Int())
	js.CopyBytesToGo(buf, u8)
	return buf
}

type promiseResult struct {
	val js.Value
	err error
}

func awaitPromise(p js.Value) (js.Value, error) {
	ch := make(chan promiseResult, 1)
	onFulfill := js.FuncOf(func(_ js.Value, args []js.Value) any {
		var v js.Value
		if len(args) > 0 {
			v = args[0]
		}
		ch <- promiseResult{val: v}
		return nil
	})
	onReject := js.FuncOf(func(_ js.Value, args []js.Value) any {
		msg := "net: webtransport promise rejected"
		if len(args) > 0 && args[0].Truthy() {
			msg = args[0].Call("toString").String()
		}
		ch <- promiseResult{err: errors.New(msg)}
		return nil
	})
	p.Call("then", onFulfill, onReject)
	res := <-ch
	onFulfill.Release()
	onReject.Release()
	return res.val, res.err
}

// Send transmits payload to the server. ChannelReliableOrdered frames travel on the bidirectional QUIC stream; ChannelUnreliable frames travel as independent QUIC datagrams (returning ErrPayloadTooLarge if payload exceeds the session's maxDatagramSize). Frames sent before the server's welcome are queued and flushed when it arrives; Send returns ErrDisconnected while the connection is down.
func (wt *WebTransportTransport) Send(to PeerID, ch Channel, payload []byte) error {
	if ch != ChannelReliableOrdered && ch != ChannelUnreliable {
		return ErrInvalidPacket
	}

	wt.mu.Lock()
	switch wt.state {
	case ConnClosed:
		wt.mu.Unlock()
		return ErrClosed
	case ConnDisconnected:
		wt.mu.Unlock()
		return ErrDisconnected
	}
	if wt.state == ConnOpen && to != PeerBroadcast && to != wt.serverID {
		wt.mu.Unlock()
		return ErrUnknownPeer
	}
	if ch == ChannelReliableOrdered && wireHeaderSize+len(payload) > wtMaxStreamMessageSize {
		wt.mu.Unlock()
		return ErrPayloadTooLarge
	}
	if ch == ChannelUnreliable && wt.state == ConnOpen && wt.maxDgram > 0 && wireHeaderSize+len(payload) > wt.maxDgram {
		wt.mu.Unlock()
		return ErrPayloadTooLarge
	}

	wt.seq[ch]++
	frame := encodeWireFrame(ch, wt.seq[ch], payload)
	if wt.state == ConnConnecting {
		wt.pending = append(wt.pending, wtPendingFrame{ch: ch, frame: frame})
		wt.mu.Unlock()
		return nil
	}

	if ch == ChannelReliableOrdered {
		framed, err := encodeStreamFrame(frame)
		if err != nil {
			wt.mu.Unlock()
			return err
		}
		wt.relOut = append(wt.relOut, framed)
		notify := wt.relNotify
		wt.mu.Unlock()
		select {
		case notify <- struct{}{}:
		default:
		}
		return nil
	}

	dgramOut := wt.dgramOut
	wt.mu.Unlock()
	select {
	case dgramOut <- frame:
	default:
		// Outgoing datagram queue is full; drop oldest to keep freshest unreliable state.
		select {
		case <-dgramOut:
		default:
		}
		select {
		case dgramOut <- frame:
		default:
		}
	}
	return nil
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

// LocalID returns the PeerID the server assigned this client, or 0 before its first welcome.
func (wt *WebTransportTransport) LocalID() PeerID {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.localID
}

// Peers returns the connected server's PeerID, or nil unless the connection is open.
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

// Err returns why the client is disconnected: a *RejectError, or ErrDisconnected / transport error. It is nil while connecting or open.
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

// Close closes the browser WebTransport session cleanly with a goodbye code so the server reports PeerLeft at once.
func (wt *WebTransportTransport) Close() error {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if wt.state == ConnClosed {
		return nil
	}
	wt.state = ConnClosed
	wt.releaseLocked(false)
	return nil
}

var (
	_ Transport     = (*WebTransportTransport)(nil)
	_ InboxReporter = (*WebTransportTransport)(nil)
	_ relayAdmitted = (*WebTransportTransport)(nil)
)
