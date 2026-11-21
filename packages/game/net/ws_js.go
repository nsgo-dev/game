//go:build js && wasm

package net

import (
	"errors"
	"sync"
	"syscall/js"
)

const (
	jsWSConnecting = 0
	jsWSOpen       = 1
)

// WebSocketTransport wraps the browser WebSocket API under WebAssembly,
// multiplexing the same 5-byte channel+seq wire header as native builds.
type WebSocketTransport struct {
	ws       js.Value
	localID  PeerID
	serverID PeerID

	mu       sync.Mutex
	seq      [2]uint32
	inbox    []Packet
	pending  [][]byte
	opened   bool
	closed   bool
	released bool

	onOpen    js.Func
	onMessage js.Func
	onClose   js.Func
	onError   js.Func
}

// DialWebSocket opens a browser WebSocket connection to rawURL (ws:// or wss://)
// and returns a Transport ready for non-blocking Send/Recv.
func DialWebSocket(rawURL string, localID PeerID) (*WebSocketTransport, error) {
	ctor := js.Global().Get("WebSocket")
	if !ctor.Truthy() {
		return nil, errors.New("net: browser WebSocket API unavailable")
	}

	ws := ctor.New(rawURL)
	ws.Set("binaryType", "arraybuffer")

	wt := &WebSocketTransport{
		ws:       ws,
		localID:  localID,
		serverID: 1,
	}

	wt.onOpen = js.FuncOf(func(this js.Value, args []js.Value) any {
		wt.mu.Lock()
		wt.opened = true
		queued := wt.pending
		wt.pending = nil
		closed := wt.closed
		wt.mu.Unlock()
		if closed {
			return nil
		}
		for _, frame := range queued {
			wt.sendRaw(frame)
		}
		return nil
	})

	wt.onMessage = js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		data := args[0].Get("data")
		if !data.Truthy() {
			return nil
		}
		u8 := js.Global().Get("Uint8Array").New(data)
		n := u8.Get("byteLength").Int()
		if n < wireHeaderSize {
			return nil
		}
		buf := make([]byte, n)
		js.CopyBytesToGo(buf, u8)
		ch, seq, payload, err := decodeWireFrame(buf)
		if err != nil {
			return nil
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
		return nil
	})

	wt.onClose = js.FuncOf(func(this js.Value, args []js.Value) any {
		wt.mu.Lock()
		wt.closed = true
		wt.pending = nil
		wt.mu.Unlock()
		return nil
	})

	wt.onError = js.FuncOf(func(this js.Value, args []js.Value) any {
		return nil
	})

	ws.Set("onopen", wt.onOpen)
	ws.Set("onmessage", wt.onMessage)
	ws.Set("onclose", wt.onClose)
	ws.Set("onerror", wt.onError)

	return wt, nil
}

func (wt *WebSocketTransport) sendRaw(frame []byte) {
	u8 := js.Global().Get("Uint8Array").New(len(frame))
	js.CopyBytesToJS(u8, frame)
	wt.ws.Call("send", u8)
}

// Send transmits payload framed with the 5-byte channel+seq header over the browser WebSocket.
// Frames sent while the socket is still connecting are queued and flushed on open.
func (wt *WebSocketTransport) Send(to PeerID, ch Channel, payload []byte) error {
	if to != PeerBroadcast && to != wt.serverID {
		return ErrUnknownPeer
	}
	if ch != ChannelReliableOrdered && ch != ChannelUnreliable {
		return ErrInvalidPacket
	}

	wt.mu.Lock()
	if wt.closed {
		wt.mu.Unlock()
		return ErrClosed
	}
	wt.seq[ch]++
	frame := encodeWireFrame(ch, wt.seq[ch], payload)
	state := wt.ws.Get("readyState").Int()
	if state == jsWSConnecting && !wt.opened {
		wt.pending = append(wt.pending, frame)
		wt.mu.Unlock()
		return nil
	}
	if state != jsWSOpen {
		wt.mu.Unlock()
		return ErrClosed
	}
	wt.mu.Unlock()

	wt.sendRaw(frame)
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

// Close closes the browser WebSocket and releases all JavaScript callback handles.
func (wt *WebSocketTransport) Close() error {
	wt.mu.Lock()
	alreadyReleased := wt.released
	wt.closed = true
	wt.pending = nil
	wt.released = true
	wt.mu.Unlock()

	if !alreadyReleased {
		wt.ws.Set("onopen", js.Null())
		wt.ws.Set("onmessage", js.Null())
		wt.ws.Set("onclose", js.Null())
		wt.ws.Set("onerror", js.Null())
		wt.ws.Call("close")
		wt.onOpen.Release()
		wt.onMessage.Release()
		wt.onClose.Release()
		wt.onError.Release()
	}
	return nil
}
