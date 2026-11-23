//go:build js && wasm

package net

import (
	"errors"
	"slices"
	"sync"
	"syscall/js"
)

const jsWSOpen = 1

// WebSocketTransport wraps the browser WebAssembly WebSocket API, multiplexing the same 5-byte channel+seq wire header as native builds.
//
// The browser connects asynchronously: DialWebSocket returns at once, sends the connection handshake when the socket opens, and the transport becomes ConnOpen when the server's welcome arrives. Until then LocalID is 0, and Send queues packets to flush after the welcome. A refusal moves it to ConnDisconnected with a *RejectError in Err.
type WebSocketTransport struct {
	rawURL string
	token  []byte

	mu       sync.Mutex
	ws       js.Value
	localID  PeerID
	serverID PeerID
	state    ConnState
	err      error
	resume   resumeState
	resumed  bool
	nonce    [hsNonceSize]byte
	seq      [2]uint32
	inbox    []Packet
	pending  [][]byte
	funcs    []js.Func

	// admissions counts handshakes that admitted the client as a new peer rather than resuming, so a RelayTransport can tell its relay session was lost even when the new ID equals the old one.
	admissions uint64
}

// DialWebSocket opens a browser WebSocket connection to rawURL (ws:// or wss://) that presents token in the connection handshake, and returns a Transport ready for non-blocking Send/Recv. The server assigns the client's ID, which LocalID reports once State is ConnOpen.
func DialWebSocket(rawURL string, token []byte) (*WebSocketTransport, error) {
	if len(token) > MaxTokenSize {
		return nil, ErrTokenTooLarge
	}
	if !js.Global().Get("WebSocket").Truthy() {
		return nil, errors.New("net: browser WebSocket API unavailable")
	}
	wt := &WebSocketTransport{rawURL: rawURL, token: slices.Clone(token), ws: js.Null()}
	wt.mu.Lock()
	err := wt.openLocked()
	wt.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return wt, nil
}

// Reconnect closes the current browser socket, if any, and opens a new one that resumes the session: within the server's grace period the client keeps its ID, and Resumed reports true once State is ConnOpen again. Past it, the server admits the client as a new peer when its token is still valid, and LocalID changes. The old socket closes with a code that tells the server to keep the session, so Reconnect works while the connection is still open too.
func (wt *WebSocketTransport) Reconnect() error {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if wt.state == ConnClosed {
		return ErrClosed
	}
	wt.releaseLocked(true)
	return wt.openLocked()
}

// openLocked creates a browser socket and wires its callbacks to this attempt only.
func (wt *WebSocketTransport) openLocked() error {
	h, err := newHello(wt.token, wt.resume)
	if err != nil {
		return err
	}
	ws := js.Global().Get("WebSocket").New(wt.rawURL)
	ws.Set("binaryType", "arraybuffer")
	wt.ws = ws
	wt.nonce = h.nonce
	wt.state = ConnConnecting
	wt.err = nil
	hello := encodeHello(h, 0)

	onOpen := js.FuncOf(func(this js.Value, args []js.Value) any {
		wt.mu.Lock()
		current := wt.ws.Equal(ws)
		wt.mu.Unlock()
		if current {
			sendJS(ws, hello)
		}
		return nil
	})
	onMessage := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		data := args[0].Get("data")
		if !data.Truthy() {
			return nil
		}
		u8 := js.Global().Get("Uint8Array").New(data)
		buf := make([]byte, u8.Get("byteLength").Int())
		js.CopyBytesToGo(buf, u8)
		wt.onMessage(ws, buf)
		return nil
	})
	onClose := js.FuncOf(func(this js.Value, args []js.Value) any {
		wt.mu.Lock()
		if wt.ws.Equal(ws) && wt.state != ConnClosed {
			if wt.state == ConnOpen || wt.err == nil {
				wt.err = ErrDisconnected
			}
			wt.state = ConnDisconnected
		}
		wt.mu.Unlock()
		return nil
	})
	onError := js.FuncOf(func(this js.Value, args []js.Value) any { return nil })
	ws.Set("onopen", onOpen)
	ws.Set("onmessage", onMessage)
	ws.Set("onclose", onClose)
	ws.Set("onerror", onError)
	wt.funcs = []js.Func{onOpen, onMessage, onClose, onError}
	return nil
}

// onMessage handles the handshake reply first and game frames after it.
func (wt *WebSocketTransport) onMessage(ws js.Value, buf []byte) {
	wt.mu.Lock()
	if !wt.ws.Equal(ws) {
		wt.mu.Unlock()
		return
	}
	switch wt.state {
	case ConnConnecting:
		w, err := acceptServerHandshake(buf, wt.nonce)
		if err != nil {
			wt.state, wt.err = ConnDisconnected, err
			wt.mu.Unlock()
			// A garbled welcome may still have admitted the client, so leave the session for Reconnect to resume.
			ws.Call("close", wsCloseReconnect)
			return
		}
		wt.localID, wt.serverID = w.peer, w.server
		wt.resume = resumeState{peer: w.peer, secret: w.secret}
		wt.resumed = w.resumed
		if !w.resumed {
			wt.admissions++
		}
		wt.state = ConnOpen
		queued := wt.pending
		wt.pending = nil
		wt.mu.Unlock()
		for _, frame := range queued {
			sendJS(ws, frame)
		}
	case ConnOpen:
		ch, seq, payload, err := decodeWireFrame(buf)
		if err == nil {
			wt.inbox = append(wt.inbox, Packet{
				From:    wt.serverID,
				Channel: ch,
				Payload: slices.Clone(payload),
				Seq:     seq,
			})
		}
		wt.mu.Unlock()
	default:
		wt.mu.Unlock()
	}
}

func sendJS(ws js.Value, frame []byte) {
	u8 := js.Global().Get("Uint8Array").New(len(frame))
	js.CopyBytesToJS(u8, frame)
	ws.Call("send", u8)
}

// releaseLocked detaches and closes the current browser socket and releases its callbacks. With reconnect it closes with wsCloseReconnect, which the server counts as a drop that keeps the session, rather than as a goodbye.
func (wt *WebSocketTransport) releaseLocked(reconnect bool) {
	if wt.ws.IsNull() || wt.ws.IsUndefined() {
		return
	}
	ws := wt.ws
	ws.Set("onopen", js.Null())
	ws.Set("onmessage", js.Null())
	ws.Set("onclose", js.Null())
	ws.Set("onerror", js.Null())
	if reconnect {
		ws.Call("close", wsCloseReconnect)
	} else {
		ws.Call("close")
	}
	for _, f := range wt.funcs {
		f.Release()
	}
	wt.funcs = nil
	wt.ws = js.Null()
}

// Send transmits payload framed with the 5-byte channel+seq header over the browser WebSocket. Frames sent before the server's welcome are queued and flushed when it arrives; Send returns ErrDisconnected while the connection is down.
func (wt *WebSocketTransport) Send(to PeerID, ch Channel, payload []byte) error {
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
	wt.seq[ch]++
	frame := encodeWireFrame(ch, wt.seq[ch], payload)
	if wt.state == ConnConnecting {
		wt.pending = append(wt.pending, frame)
		wt.mu.Unlock()
		return nil
	}
	ws := wt.ws
	wt.mu.Unlock()
	if ws.Get("readyState").Int() != jsWSOpen {
		return ErrDisconnected
	}
	sendJS(ws, frame)
	return nil
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

// LocalID returns the PeerID the server assigned this client, or 0 before its first welcome.
func (wt *WebSocketTransport) LocalID() PeerID {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.localID
}

// Peers returns the connected server's PeerID, or nil unless the connection is open.
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

// Err returns why the client is disconnected: a *RejectError, or ErrDisconnected. It is nil while connecting or open.
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

// Close closes the browser WebSocket and releases all JavaScript callback handles. The browser sends a close frame, so the server reports PeerLeft at once.
func (wt *WebSocketTransport) Close() error {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if wt.state == ConnClosed {
		return nil
	}
	wt.state = ConnClosed
	wt.pending = nil
	wt.releaseLocked(false)
	return nil
}
