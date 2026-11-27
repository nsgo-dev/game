//go:build js && wasm

package wsclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"syscall/js"
)

const jsWSOpen = 1

// Client is a plain WebSocket client connected to any ws:// or wss:// server, backed by the browser's WebSocket API. The browser answers pings on its own and delivers whole messages, so Recv, State and Close simply read what the callbacks queued.
type Client struct {
	mu          sync.Mutex
	ws          js.Value
	state       State
	err         error
	proto       string
	maxMessage  int
	maxInbox    int
	inbox       []Message
	dropped     int
	closeCode   int
	closeReason string
	funcs       []js.Func
}

// Dial opens a browser WebSocket connection to rawURL (ws:// or wss://) and returns the client once it is open. ctx bounds the wait. Browsers connect asynchronously, so Dial suspends until the socket opens, fails, or ctx ends; while it waits the page's event loop keeps running, but the goroutine that called Dial does not, so dial during loading or from a worker, not inside a frame callback that must return. Options.Header is unsupported in the browser and fails the Dial with ErrHeadersUnsupported.
func Dial(ctx context.Context, rawURL string, opts Options) (*Client, error) {
	if _, err := checkURL(rawURL); err != nil {
		return nil, err
	}
	if len(opts.Header) > 0 {
		return nil, ErrHeadersUnsupported
	}
	if !js.Global().Get("WebSocket").Truthy() {
		return nil, errors.New("net: browser WebSocket API unavailable")
	}
	maxMessage := opts.MaxMessage
	if maxMessage <= 0 {
		maxMessage = DefaultMaxMessage
	}
	maxInbox := opts.MaxInbox
	if maxInbox == 0 {
		maxInbox = DefaultMaxInbox
	}

	c := &Client{
		state:      Open,
		maxMessage: maxMessage,
		maxInbox:   maxInbox,
		closeCode:  CloseAbnormal,
	}

	var ws js.Value
	ctor := js.Global().Get("WebSocket")
	if len(opts.Protocols) > 0 {
		arr := js.Global().Get("Array").New(len(opts.Protocols))
		for i, p := range opts.Protocols {
			arr.SetIndex(i, p)
		}
		ws = ctor.New(rawURL, arr)
	} else {
		ws = ctor.New(rawURL)
	}
	ws.Set("binaryType", "arraybuffer")
	c.ws = ws

	// opened resolves the Dial: nil once the socket is open, or why it failed. Every callback sends without blocking, and only the first send lands.
	opened := make(chan error, 1)
	fail := func(err error) {
		select {
		case opened <- err:
		default:
		}
	}

	onOpen := js.FuncOf(func(this js.Value, args []js.Value) any {
		c.mu.Lock()
		c.proto = ws.Get("protocol").String()
		c.mu.Unlock()
		fail(nil)
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
		var msg Message
		if data.Type() == js.TypeString {
			msg = Message{Text: true, Data: []byte(data.String())}
		} else {
			u8 := js.Global().Get("Uint8Array").New(data)
			msg.Data = make([]byte, u8.Get("byteLength").Int())
			js.CopyBytesToGo(msg.Data, u8)
		}
		if len(msg.Data) > c.maxMessage {
			c.mu.Lock()
			c.state = Closed
			c.err = ErrMessageTooLarge
			c.closeCode, c.closeReason = CloseMessageTooBig, ""
			c.mu.Unlock()
			ws.Call("close", CloseMessageTooBig)
			return nil
		}
		c.mu.Lock()
		if c.state == Open {
			c.inbox = push(c.inbox, c.maxInbox, &c.dropped, msg)
		}
		c.mu.Unlock()
		return nil
	})
	onClose := js.FuncOf(func(this js.Value, args []js.Value) any {
		code, reason := CloseAbnormal, ""
		if len(args) > 0 {
			code = args[0].Get("code").Int()
			reason = args[0].Get("reason").String()
		}
		c.mu.Lock()
		if c.state != Closed {
			c.state = Closed
			if c.err == nil && code == CloseAbnormal {
				c.err = errors.New("net: websocket connection lost")
			}
			c.closeCode, c.closeReason = code, reason
		}
		c.mu.Unlock()
		fail(fmt.Errorf("net: websocket closed before it opened (code %d)", code))
		return nil
	})
	onError := js.FuncOf(func(this js.Value, args []js.Value) any {
		// A close event always follows an error, and it carries the code; this only resolves a Dial whose server never answered.
		fail(errors.New("net: websocket connection failed"))
		return nil
	})
	ws.Set("onopen", onOpen)
	ws.Set("onmessage", onMessage)
	ws.Set("onclose", onClose)
	ws.Set("onerror", onError)
	c.funcs = []js.Func{onOpen, onMessage, onClose, onError}

	select {
	case err := <-opened:
		if err != nil {
			c.release()
			return nil, err
		}
		return c, nil
	case <-ctx.Done():
		c.release()
		ws.Call("close")
		return nil, ctx.Err()
	}
}

// release detaches the socket's callbacks and releases their handles.
func (c *Client) release() {
	c.ws.Set("onopen", js.Null())
	c.ws.Set("onmessage", js.Null())
	c.ws.Set("onclose", js.Null())
	c.ws.Set("onerror", js.Null())
	for _, f := range c.funcs {
		f.Release()
	}
	c.funcs = nil
}

// Send queues payload as one binary message. It returns ErrClosed once the connection is closed and ErrMessageTooLarge for a payload over Options.MaxMessage.
func (c *Client) Send(payload []byte) error {
	if err := c.sendable(len(payload)); err != nil {
		return err
	}
	u8 := js.Global().Get("Uint8Array").New(len(payload))
	js.CopyBytesToJS(u8, payload)
	c.ws.Call("send", u8)
	return nil
}

// SendText queues text as one UTF-8 text message, with the same errors as Send.
func (c *Client) SendText(text string) error {
	if err := c.sendable(len(text)); err != nil {
		return err
	}
	c.ws.Call("send", text)
	return nil
}

func (c *Client) sendable(n int) error {
	if n > c.maxMessage {
		return ErrMessageTooLarge
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == Closed {
		return ErrClosed
	}
	if c.ws.Get("readyState").Int() != jsWSOpen {
		return ErrClosed
	}
	return nil
}

// Recv pops the oldest received message without blocking, reporting ok false when the inbox is empty.
func (c *Client) Recv() (msg Message, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	msg, c.inbox, ok = pop(c.inbox)
	return msg, ok
}

// Buffered returns the number of received messages waiting for Recv.
func (c *Client) Buffered() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.inbox)
}

// Dropped returns how many received messages were dropped because the inbox was at its Options.MaxInbox bound.
func (c *Client) Dropped() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dropped
}

// State returns whether the connection is Open or Closed.
func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Protocol returns the subprotocol the server selected at the handshake, or "" for none.
func (c *Client) Protocol() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.proto
}

// Err returns why the connection closed: nil for a clean close, or the failure otherwise.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// CloseStatus returns the status code and reason of the close that ended the connection: the peer's when it closed first, otherwise the ones Close was called with. The code is CloseNoStatus when the close frame carried none, CloseAbnormal when the connection dropped with no close frame at all, and 0 while the connection is still open.
func (c *Client) CloseStatus() (code int, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != Closed {
		return 0, ""
	}
	return c.closeCode, c.closeReason
}

// Close closes the browser WebSocket with code and reason; the browser sends the close frame. A zero code sends no status code, and CloseStatus then reports CloseNoStatus. It is a no-op on an already closed client.
func (c *Client) Close(code int, reason string) error {
	if !validCloseCode(code) {
		return ErrInvalidCloseCode
	}
	c.mu.Lock()
	if c.state == Closed {
		c.mu.Unlock()
		return nil
	}
	c.state = Closed
	c.closeCode, c.closeReason = code, reason
	if code == 0 {
		c.closeCode = CloseNoStatus
	}
	c.mu.Unlock()

	c.release()
	if code == 0 {
		c.ws.Call("close")
	} else {
		c.ws.Call("close", code, reason)
	}
	return nil
}
