//go:build !js

package wsclient

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nsgo-dev/game/pkg/online/internal/wsproto"
)

const (
	dialTimeout      = 10 * time.Second
	handshakeTimeout = 10 * time.Second
	writeTimeout     = 10 * time.Second
)

// Client is a plain RFC 6455 WebSocket client connected to any ws:// or wss:// server. Dial returns it open; the read side runs on its own goroutine, so Recv, State and Close are safe to call from the game loop while Send and SendText are safe from any goroutine.
type Client struct {
	writeMu sync.Mutex
	closing atomic.Bool // set by Close, so a blocked write breaks instead of holding Close up for writeTimeout

	mu          sync.Mutex
	conn        net.Conn
	state       State
	err         error
	proto       string
	maxMessage  uint64
	maxInbox    int
	inbox       []Message
	dropped     int
	closeCode   int
	closeReason string
}

// Dial opens a WebSocket connection to rawURL (ws:// or wss://) and returns the client once the server's upgrade response arrives, or an error when the dial, TLS or handshake fails. ctx bounds the dial and the handshake; after that the connection has no idle timeout, because a third-party server may stay silent for as long as it likes. The client answers the server's pings with pongs on its own.
func Dial(ctx context.Context, rawURL string, opts Options) (*Client, error) {
	u, err := checkURL(rawURL)
	if err != nil {
		return nil, err
	}
	maxMessage := opts.MaxMessage
	if maxMessage <= 0 {
		maxMessage = DefaultMaxMessage
	}
	maxInbox := opts.MaxInbox
	if maxInbox == 0 {
		maxInbox = DefaultMaxInbox
	}
	host, err := wsproto.HostPort(u)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{Timeout: dialTimeout}
	var conn net.Conn
	if u.Scheme == "wss" {
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: u.Hostname()}}
		conn, err = tlsDialer.DialContext(ctx, "tcp", host)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(handshakeTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	br := bufio.NewReader(conn)
	proto, err := wsproto.ClientUpgrade(conn, br, u, opts.Header, opts.Protocols)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})

	c := &Client{
		conn:       conn,
		state:      Open,
		proto:      proto,
		maxMessage: uint64(maxMessage),
		maxInbox:   maxInbox,
		closeCode:  CloseAbnormal,
	}
	go c.readLoop(conn, br)
	return c, nil
}

// writeFrame writes one masked frame with writeTimeout as its deadline, unless Close began, in which case it reports ErrClosed at once. c.conn is set once in Dial and never changes, so it needs no lock here.
func (c *Client) writeFrame(opcode byte, payload []byte) error {
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if c.closing.Load() {
		_ = c.conn.SetWriteDeadline(time.Now())
		return ErrClosed
	}
	err := wsproto.WriteFrameLimit(c.conn, opcode, true, payload, c.maxMessage)
	_ = c.conn.SetWriteDeadline(time.Time{})
	return err
}

// readLoop reads frames until the connection ends, answering pings, reassembling fragmented messages and queueing whole messages for Recv.
func (c *Client) readLoop(conn net.Conn, br *bufio.Reader) {
	cause := error(nil)
	code, reason := CloseAbnormal, ""
	defer func() {
		c.mu.Lock()
		if c.state != Closed {
			c.state = Closed
			c.err = cause
			c.closeCode, c.closeReason = code, reason
		}
		c.mu.Unlock()
		_ = conn.Close()
	}()

	var fragBuf []byte
	fragText := false
	inFrag := false

	for {
		fin, op, payload, _, err := wsproto.ReadFrameLimit(br, c.maxMessage)
		if err != nil {
			if errors.Is(err, wsproto.ErrPayloadTooLarge) {
				cause = ErrMessageTooLarge
				code, reason = CloseMessageTooBig, ""
				c.closeWith(CloseMessageTooBig, "")
			} else {
				cause = err
			}
			return
		}
		switch op {
		case wsproto.OpPing:
			c.writeMu.Lock()
			_ = c.writeFrame(wsproto.OpPong, payload)
			c.writeMu.Unlock()
		case wsproto.OpPong:
			continue
		case wsproto.OpClose:
			code, reason = parseClosePayload(payload)
			if !c.closing.Load() {
				// Echo the close, as RFC 6455 §7 asks an endpoint that did not start the closing handshake to.
				c.writeMu.Lock()
				_ = c.writeFrame(wsproto.OpClose, payload)
				c.writeMu.Unlock()
			}
			return
		case wsproto.OpText, wsproto.OpBinary:
			if inFrag {
				cause = ErrProtocol
				code, reason = CloseProtocolError, ""
				c.closeWith(CloseProtocolError, "")
				return
			}
			if !fin {
				fragText = op == wsproto.OpText
				fragBuf = append(fragBuf[:0], payload...)
				inFrag = true
				continue
			}
			c.deliver(op == wsproto.OpText, payload)
		case wsproto.OpContinuation:
			if !inFrag {
				cause = ErrProtocol
				code, reason = CloseProtocolError, ""
				c.closeWith(CloseProtocolError, "")
				return
			}
			if uint64(len(fragBuf)+len(payload)) > c.maxMessage {
				cause = ErrMessageTooLarge
				code, reason = CloseMessageTooBig, ""
				c.closeWith(CloseMessageTooBig, "")
				return
			}
			fragBuf = append(fragBuf, payload...)
			if fin {
				cp := make([]byte, len(fragBuf))
				copy(cp, fragBuf)
				c.deliver(fragText, cp)
				fragBuf = fragBuf[:0]
				inFrag = false
			}
		}
	}
}

// closeWith sends a close frame on a protocol-level failure, best effort.
func (c *Client) closeWith(code int, reason string) {
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload, uint16(code))
	copy(payload[2:], reason)
	c.writeMu.Lock()
	_ = c.writeFrame(wsproto.OpClose, payload)
	c.writeMu.Unlock()
}

// parseClosePayload returns the status code and reason of a close frame's payload, or CloseNoStatus when it has none.
func parseClosePayload(payload []byte) (code int, reason string) {
	if len(payload) < 2 {
		return CloseNoStatus, ""
	}
	return int(binary.BigEndian.Uint16(payload)), string(payload[2:])
}

// deliver queues a whole message for Recv.
func (c *Client) deliver(text bool, data []byte) {
	c.mu.Lock()
	if c.state == Open {
		c.inbox = push(c.inbox, c.maxInbox, &c.dropped, Message{Text: text, Data: data})
	}
	c.mu.Unlock()
}

// Send queues payload as one binary message. It returns ErrClosed once the connection is closed and ErrMessageTooLarge for a payload over Options.MaxMessage.
func (c *Client) Send(payload []byte) error {
	return c.send(wsproto.OpBinary, payload)
}

// SendText queues text as one UTF-8 text message, with the same errors as Send.
func (c *Client) SendText(text string) error {
	return c.send(wsproto.OpText, []byte(text))
}

func (c *Client) send(opcode byte, payload []byte) error {
	if uint64(len(payload)) > c.maxMessage {
		return ErrMessageTooLarge
	}
	c.mu.Lock()
	state := c.state
	c.mu.Unlock()
	if state == Closed {
		return ErrClosed
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrame(opcode, payload)
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

// Err returns why the connection closed: nil for a clean close, ErrMessageTooLarge or ErrProtocol for one the client ended, or the read error for a dropped connection.
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

// Close sends a close frame with code and reason and closes the connection. A zero code sends no status code, and CloseStatus then reports CloseNoStatus. It is a no-op on an already closed client.
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
	conn := c.conn
	c.closeCode, c.closeReason = code, reason
	if code == 0 {
		c.closeCode = CloseNoStatus
	}
	c.mu.Unlock()
	if conn == nil {
		return nil
	}

	// Break a Send already blocked on a server that does not read, then send the close frame with its own short deadline.
	c.closing.Store(true)
	_ = conn.SetWriteDeadline(time.Now())
	var payload []byte
	if code != 0 {
		payload = make([]byte, 2+len(reason))
		binary.BigEndian.PutUint16(payload, uint16(code))
		copy(payload[2:], reason)
	}
	c.writeMu.Lock()
	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	_ = wsproto.WriteFrame(conn, wsproto.OpClose, true, payload)
	c.writeMu.Unlock()
	return conn.Close()
}
