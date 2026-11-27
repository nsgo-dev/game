// Package wsclient provides a plain RFC 6455 WebSocket client for talking to servers that are not a netplay WebSocketServer: third-party game backends such as Nakama or Colyseus, chat integrations, hosted realtime services, or a custom JSON protocol shared with a web dashboard. It offers the same API on native targets and in the browser (js/wasm), built on the shared framing of pkg/online/internal/wsproto.
//
// To talk to a server written with this repository's netplay package, use netplay.DialWebSocket instead: it speaks netplay's own handshake and channel framing on top of WebSocket, which a plain client does not.
package wsclient

import (
	"errors"
	"net/http"
	"net/url"
)

// State reports where a Client is in its lifecycle. A client is Open once Dial returns it and moves to Closed when either side closes the connection or the connection drops; a plain client does not reconnect, so a Closed client is done and a new one takes its place.
type State int

const (
	// Open means the connection is up and Send, SendText and Recv operate on it.
	Open State = iota
	// Closed means the connection ended, cleanly or not; CloseStatus and Err report how.
	Closed
)

// RFC 6455 §7.4 close status codes a game most often sends or reads from CloseStatus.
const (
	CloseNormal        = 1000 // the purpose of the connection is fulfilled
	CloseGoingAway     = 1001 // server going down, or the page is navigating away
	CloseProtocolError = 1002 // the peer broke the protocol
	CloseUnsupported   = 1003 // the peer sent a data type it does not accept
	CloseNoStatus      = 1005 // reserved: the close frame carried no code
	CloseAbnormal      = 1006 // reserved: the connection dropped with no close frame at all
	CloseMessageTooBig = 1009 // the peer sent a message too big to process
)

// Message is one WebSocket message received from the server. Text reports whether it arrived as a text frame; Data holds its payload either way.
type Message struct {
	Text bool
	Data []byte
}

// Options tunes Dial. The zero value is a plain connection with default limits.
type Options struct {
	// Protocols lists the subprotocols the client offers, in preference order; the server picks one, which Protocol reports, or none. Browsers support subprotocols on every target.
	Protocols []string
	// Header carries extra handshake request headers, such as an Authorization token. It is native only: the browser WebSocket API cannot set headers, so a browser Dial with a non-empty Header fails with ErrHeadersUnsupported instead of silently dropping them. Entries naming a header the handshake writes itself (Host, Upgrade, Connection, Sec-WebSocket-*) are ignored.
	Header http.Header
	// MaxMessage is the largest message, in bytes, the client receives or sends. Zero uses DefaultMaxMessage. A larger incoming message is refused: the connection closes and Err reports ErrMessageTooLarge.
	MaxMessage int
	// MaxInbox is the most received messages the client queues for Recv. Zero uses DefaultMaxInbox; a negative value removes the bound. Past the bound the oldest queued message is dropped to make room, and Dropped counts the loss.
	MaxInbox int
}

const (
	// DefaultMaxMessage bounds a received or sent message when Options.MaxMessage is zero.
	DefaultMaxMessage = 4 << 20 // 4 MiB, as in netplay
	// DefaultMaxInbox bounds the messages queued for Recv when Options.MaxInbox is zero.
	DefaultMaxInbox = 256
)

var (
	// ErrClosed is returned when operating on a closed client.
	ErrClosed = errors.New("net: websocket client closed")
	// ErrMessageTooLarge is returned by Send and SendText for a payload over Options.MaxMessage, and reported by Err when an incoming message passes it.
	ErrMessageTooLarge = errors.New("net: websocket message too large")
	// ErrProtocol is reported by Err when the server broke RFC 6455, such as starting a new message before finishing a fragmented one.
	ErrProtocol = errors.New("net: websocket protocol error")
	// ErrHeadersUnsupported is returned by Dial in the browser when Options.Header is non-empty: the browser WebSocket API cannot set handshake headers.
	ErrHeadersUnsupported = errors.New("net: websocket handshake headers are not supported in the browser")
	// ErrInvalidCloseCode is returned by Close for a code outside the ranges RFC 6455 §7.4 allows an endpoint to send.
	ErrInvalidCloseCode = errors.New("net: invalid websocket close code")
)

// checkURL validates the WebSocket URL before any platform dials it: ws:// or wss:// with a host.
func checkURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, errors.New("net: websocket url needs a ws:// or wss:// scheme")
	}
	if u.Hostname() == "" {
		return nil, errors.New("net: websocket url has no host")
	}
	return u, nil
}

// validCloseCode reports whether code may go out in a close frame (RFC 6455 §7.4): 0 means no code, libraries use 1000-1014 apart from the reserved 1004-1006, and applications use 3000-4999.
func validCloseCode(code int) bool {
	if code == 0 {
		return true
	}
	if code >= 1000 && code <= 1014 {
		return code != 1004 && code != 1005 && code != 1006
	}
	return code >= 3000 && code <= 4999
}

// push queues a received message, dropping the oldest when the inbox is at its bound; a negative maxInbox is unbounded.
func push(inbox []Message, maxInbox int, dropped *int, msg Message) []Message {
	if maxInbox >= 0 && len(inbox) >= maxInbox {
		inbox[0] = Message{}
		inbox = inbox[1:]
		*dropped++
	}
	return append(inbox, msg)
}

// pop takes the oldest queued message, releasing the backing array once the inbox drains.
func pop(inbox []Message) (Message, []Message, bool) {
	if len(inbox) == 0 {
		return Message{}, inbox, false
	}
	msg := inbox[0]
	inbox[0] = Message{}
	inbox = inbox[1:]
	if len(inbox) == 0 {
		inbox = nil
	}
	return msg, inbox, true
}
