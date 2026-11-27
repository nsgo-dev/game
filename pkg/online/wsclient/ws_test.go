//go:build !js

package wsclient_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/internal/wsproto"
	"github.com/nsgo-dev/game/pkg/online/wsclient"
)

// testServer is an in-process RFC 6455 server for one client: it answers the upgrade and hands the connection to handle, which drives the test's side of the conversation. It uses the same wsproto framing the client is built on, so a bug shared by both sides would cancel out only if it were symmetric, and the fragmented-frame and raw close-code tests write their frames by hand to stay honest.
type testServer struct {
	t   *testing.T
	ln  net.Listener
	req chan *http.Request
	err chan error
}

// newTestServer starts a server that accepts one connection, parses the upgrade request, sends it to the req channel and runs handle with the upgraded connection. respond writes the 101 answer; the default is a bare accept, and tests override it to negotiate a subprotocol or refuse the upgrade.
func newTestServer(t *testing.T, respond func(req *http.Request) string, handle func(conn net.Conn, br *bufio.Reader)) *testServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if respond == nil {
		respond = func(*http.Request) string { return "" }
	}
	s := &testServer{t: t, ln: ln, req: make(chan *http.Request, 1), err: make(chan error, 1)}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		br := bufio.NewReader(conn)
		req, err := http.ReadRequest(br)
		if err != nil {
			s.err <- fmt.Errorf("read request: %w", err)
			_ = conn.Close()
			return
		}
		s.req <- req
		answer := respond(req)
		if answer == "" {
			key := strings.TrimSpace(req.Header.Get("Sec-WebSocket-Key"))
			answer = "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: " + wsproto.AcceptKey(key) + "\r\n\r\n"
		}
		if _, err := io.WriteString(conn, answer); err != nil {
			s.err <- fmt.Errorf("write response: %w", err)
			_ = conn.Close()
			return
		}
		if !strings.Contains(answer, "101") {
			_ = conn.Close()
			return
		}
		handle(conn, br)
	}()
	return s
}

func (s *testServer) url() string { return "ws://" + s.ln.Addr().String() + "/socket" }

func (s *testServer) close() { _ = s.ln.Close() }

// request waits for the client's upgrade request.
func (s *testServer) request() *http.Request {
	s.t.Helper()
	select {
	case req := <-s.req:
		return req
	case err := <-s.err:
		s.t.Fatalf("server: %v", err)
	case <-time.After(2 * time.Second):
		s.t.Fatal("timed out waiting for the upgrade request")
	}
	return nil
}

// echo reads frames until the client closes and answers: pings with pongs, text and binary messages with an identical message, and a close with a close echoing the same payload.
func echo(conn net.Conn, br *bufio.Reader) {
	for {
		_, op, payload, _, err := wsproto.ReadFrame(br)
		if err != nil {
			_ = conn.Close()
			return
		}
		switch op {
		case wsproto.OpPing:
			_ = wsproto.WriteFrame(conn, wsproto.OpPong, false, payload)
		case wsproto.OpText, wsproto.OpBinary:
			_ = wsproto.WriteFrame(conn, op, false, payload)
		case wsproto.OpClose:
			_ = wsproto.WriteFrame(conn, wsproto.OpClose, false, payload)
			_ = conn.Close()
			return
		}
	}
}

func dial(t *testing.T, url string, opts wsclient.Options) *wsclient.Client {
	t.Helper()
	c, err := wsclient.Dial(context.Background(), url, opts)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return c
}

func waitRecv(t *testing.T, c *wsclient.Client) wsclient.Message {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if msg, ok := c.Recv(); ok {
			return msg
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for a message")
	return wsclient.Message{}
}

func waitState(t *testing.T, c *wsclient.Client, want wsclient.State) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.State() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for state %v", want)
}

func TestEchoTextAndBinary(t *testing.T) {
	s := newTestServer(t, nil, echo)
	defer s.close()
	c := dial(t, s.url(), wsclient.Options{})
	defer func() { _ = c.Close(0, "") }()
	s.request()

	if c.State() != wsclient.Open {
		t.Fatalf("State = %v, want Open", c.State())
	}
	if c.Protocol() != "" {
		t.Fatalf("Protocol = %q, want none", c.Protocol())
	}
	if code, _ := c.CloseStatus(); code != 0 {
		t.Fatalf("CloseStatus on an open client = %d, want 0", code)
	}

	if err := c.SendText("hello server"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if msg := waitRecv(t, c); !msg.Text || string(msg.Data) != "hello server" {
		t.Fatalf("text echo = %+v", msg)
	}

	// A binary message past the 126-byte extended length.
	big := bytes.Repeat([]byte{0xAB}, 300)
	if err := c.Send(big); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if msg := waitRecv(t, c); msg.Text || !bytes.Equal(msg.Data, big) {
		t.Fatalf("binary echo: text=%v len=%d", msg.Text, len(msg.Data))
	}
	if c.Buffered() != 0 {
		t.Fatalf("Buffered = %d after draining, want 0", c.Buffered())
	}
}

func TestServerPingAnswered(t *testing.T) {
	ponged := make(chan []byte, 1)
	s := newTestServer(t, nil, func(conn net.Conn, br *bufio.Reader) {
		_ = wsproto.WriteFrame(conn, wsproto.OpPing, false, []byte("tick"))
		_, op, payload, _, err := wsproto.ReadFrame(br)
		if err == nil && op == wsproto.OpPong {
			ponged <- payload
		} else {
			ponged <- nil
		}
	})
	defer s.close()
	c := dial(t, s.url(), wsclient.Options{})
	defer func() { _ = c.Close(0, "") }()
	s.request()

	select {
	case payload := <-ponged:
		if string(payload) != "tick" {
			t.Fatalf("pong payload = %q, want %q", payload, "tick")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client did not answer the server's ping")
	}
}

// writeRawFrame writes a server frame with an explicit FIN bit, which wsproto.WriteFrame cannot express, for the fragmentation test.
func writeRawFrame(conn net.Conn, fin bool, opcode byte, payload []byte) error {
	var hdr [2]byte
	if fin {
		hdr[0] = 0x80
	}
	hdr[0] |= opcode
	hdr[1] = byte(len(payload))
	if _, err := conn.Write(hdr[:]); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

func TestFragmentedMessage(t *testing.T) {
	s := newTestServer(t, nil, func(conn net.Conn, br *bufio.Reader) {
		_ = writeRawFrame(conn, false, wsproto.OpText, []byte("hello "))
		_ = writeRawFrame(conn, false, wsproto.OpContinuation, []byte("fragmented "))
		_ = writeRawFrame(conn, true, wsproto.OpContinuation, []byte("world"))
		_ = writeRawFrame(conn, false, wsproto.OpBinary, []byte{1})
		_ = writeRawFrame(conn, true, wsproto.OpContinuation, []byte{2})
		echo(conn, br)
	})
	defer s.close()
	c := dial(t, s.url(), wsclient.Options{})
	defer func() { _ = c.Close(0, "") }()
	s.request()

	if msg := waitRecv(t, c); !msg.Text || string(msg.Data) != "hello fragmented world" {
		t.Fatalf("fragmented text = %+v", msg)
	}
	if msg := waitRecv(t, c); msg.Text || !bytes.Equal(msg.Data, []byte{1, 2}) {
		t.Fatalf("fragmented binary = %+v", msg)
	}
}

func TestServerCloseCarriesCodeAndReason(t *testing.T) {
	s := newTestServer(t, nil, func(conn net.Conn, br *bufio.Reader) {
		_ = wsproto.WriteFrame(conn, wsproto.OpText, false, []byte("last call"))
		payload := make([]byte, 2+len("going away"))
		binary.BigEndian.PutUint16(payload, wsclient.CloseGoingAway)
		copy(payload[2:], "going away")
		_ = wsproto.WriteFrame(conn, wsproto.OpClose, false, payload)
		// The client echoes the close; read it so the test sees a full handshake.
		_, _, _, _, _ = wsproto.ReadFrame(br)
		_ = conn.Close()
	})
	defer s.close()
	c := dial(t, s.url(), wsclient.Options{})
	s.request()

	if msg := waitRecv(t, c); !msg.Text || string(msg.Data) != "last call" {
		t.Fatalf("message before close = %+v", msg)
	}
	waitState(t, c, wsclient.Closed)
	if code, reason := c.CloseStatus(); code != wsclient.CloseGoingAway || reason != "going away" {
		t.Fatalf("CloseStatus = (%d, %q), want (1001, %q)", code, reason, "going away")
	}
	if err := c.Err(); err != nil {
		t.Fatalf("Err after a clean close = %v, want nil", err)
	}
	if err := c.SendText("too late"); !errors.Is(err, wsclient.ErrClosed) {
		t.Fatalf("Send on a closed client = %v, want ErrClosed", err)
	}
	if err := c.Close(1000, ""); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}
}

func TestClientCloseCarriesCodeAndReason(t *testing.T) {
	got := make(chan string, 1)
	s := newTestServer(t, nil, func(conn net.Conn, br *bufio.Reader) {
		_, op, payload, _, err := wsproto.ReadFrame(br)
		if err != nil || op != wsproto.OpClose {
			got <- fmt.Sprintf("op=%d err=%v", op, err)
			return
		}
		code := 0
		if len(payload) >= 2 {
			code = int(binary.BigEndian.Uint16(payload))
		}
		got <- fmt.Sprintf("%d %s", code, payload[2:])
		_ = wsproto.WriteFrame(conn, wsproto.OpClose, false, payload)
		_ = conn.Close()
	})
	defer s.close()
	c := dial(t, s.url(), wsclient.Options{})
	s.request()

	if err := c.Close(wsclient.CloseNormal, "done"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case got := <-got:
		if got != "1000 done" {
			t.Fatalf("server saw close %q, want %q", got, "1000 done")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never saw the close frame")
	}
	if c.State() != wsclient.Closed {
		t.Fatalf("State = %v, want Closed", c.State())
	}
	if code, reason := c.CloseStatus(); code != wsclient.CloseNormal || reason != "done" {
		t.Fatalf("CloseStatus = (%d, %q), want (1000, %q)", code, reason, "done")
	}
}

func TestSubprotocolNegotiated(t *testing.T) {
	s := newTestServer(t, func(req *http.Request) string {
		offered := req.Header.Get("Sec-WebSocket-Protocol")
		if offered != "v2.json, v1.json" {
			return "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n"
		}
		key := strings.TrimSpace(req.Header.Get("Sec-WebSocket-Key"))
		return "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + wsproto.AcceptKey(key) + "\r\n" +
			"Sec-WebSocket-Protocol: v1.json\r\n\r\n"
	}, echo)
	defer s.close()
	c := dial(t, s.url(), wsclient.Options{Protocols: []string{"v2.json", "v1.json"}})
	defer func() { _ = c.Close(0, "") }()
	s.request()

	if c.Protocol() != "v1.json" {
		t.Fatalf("Protocol = %q, want %q", c.Protocol(), "v1.json")
	}
}

func TestSubprotocolMismatchFailsDial(t *testing.T) {
	s := newTestServer(t, func(req *http.Request) string {
		key := strings.TrimSpace(req.Header.Get("Sec-WebSocket-Key"))
		return "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + wsproto.AcceptKey(key) + "\r\n" +
			"Sec-WebSocket-Protocol: never-offered\r\n\r\n"
	}, echo)
	defer s.close()

	_, err := wsclient.Dial(context.Background(), s.url(), wsclient.Options{Protocols: []string{"v1.json"}})
	if err == nil || !strings.Contains(err.Error(), "never-offered") {
		t.Fatalf("Dial = %v, want an unrequested-subprotocol error", err)
	}
}

func TestHandshakeHeaderSent(t *testing.T) {
	s := newTestServer(t, nil, echo)
	defer s.close()
	c := dial(t, s.url(), wsclient.Options{
		Header: http.Header{"Authorization": {"Bearer tok-123"}},
	})
	defer func() { _ = c.Close(0, "") }()
	req := s.request()

	if got := req.Header.Get("Authorization"); got != "Bearer tok-123" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer tok-123")
	}
}

func TestUpgradeRefusedFailsDial(t *testing.T) {
	s := newTestServer(t, func(req *http.Request) string {
		return "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n"
	}, echo)
	defer s.close()

	_, err := wsclient.Dial(context.Background(), s.url(), wsclient.Options{})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("Dial = %v, want a status-403 error", err)
	}
}

func TestInvalidURL(t *testing.T) {
	for _, url := range []string{"http://example.com/socket", "ws://", "://bad"} {
		if _, err := wsclient.Dial(context.Background(), url, wsclient.Options{}); err == nil {
			t.Fatalf("Dial(%q) = nil error, want one", url)
		}
	}
}

func TestOversizedMessageRefused(t *testing.T) {
	s := newTestServer(t, nil, func(conn net.Conn, br *bufio.Reader) {
		_ = wsproto.WriteFrame(conn, wsproto.OpBinary, false, make([]byte, 128))
		// The client closes with 1009 message too big.
		_, op, payload, _, _ := wsproto.ReadFrame(br)
		if op == wsproto.OpClose && len(payload) >= 2 {
			_ = wsproto.WriteFrame(conn, wsproto.OpClose, false, payload)
		}
		_ = conn.Close()
	})
	defer s.close()
	c := dial(t, s.url(), wsclient.Options{MaxMessage: 64})
	s.request()

	waitState(t, c, wsclient.Closed)
	if err := c.Err(); !errors.Is(err, wsclient.ErrMessageTooLarge) {
		t.Fatalf("Err = %v, want ErrMessageTooLarge", err)
	}
	if msg, ok := c.Recv(); ok {
		t.Fatalf("Recv = %+v after the oversized message, want nothing", msg)
	}
	if code, _ := c.CloseStatus(); code != wsclient.CloseMessageTooBig {
		t.Fatalf("CloseStatus code = %d, want 1009", code)
	}
}

func TestSendTooLarge(t *testing.T) {
	s := newTestServer(t, nil, echo)
	defer s.close()
	c := dial(t, s.url(), wsclient.Options{MaxMessage: 64})
	defer func() { _ = c.Close(0, "") }()
	s.request()

	if err := c.Send(make([]byte, 65)); !errors.Is(err, wsclient.ErrMessageTooLarge) {
		t.Fatalf("Send = %v, want ErrMessageTooLarge", err)
	}
	if err := c.Send(make([]byte, 64)); err != nil {
		t.Fatalf("Send at the bound = %v", err)
	}
}

func TestInboxBoundDropsOldest(t *testing.T) {
	sent := make(chan struct{})
	s := newTestServer(t, nil, func(conn net.Conn, br *bufio.Reader) {
		for i := 0; i < 4; i++ {
			_ = wsproto.WriteFrame(conn, wsproto.OpText, false, []byte(fmt.Sprintf("msg-%d", i)))
		}
		close(sent)
		echo(conn, br)
	})
	defer s.close()
	c := dial(t, s.url(), wsclient.Options{MaxInbox: 2})
	s.request()
	<-sent

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && c.Dropped() < 2 {
		time.Sleep(time.Millisecond)
	}
	if c.Dropped() != 2 || c.Buffered() != 2 {
		t.Fatalf("Dropped = %d, Buffered = %d; want 2 and 2", c.Dropped(), c.Buffered())
	}
	for _, want := range []string{"msg-2", "msg-3"} {
		if msg := waitRecv(t, c); string(msg.Data) != want {
			t.Fatalf("Recv = %q, want %q (oldest dropped)", msg.Data, want)
		}
	}
	_ = c.Close(0, "")
}

func TestDialContextCanceled(t *testing.T) {
	s := newTestServer(t, nil, echo)
	defer s.close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := wsclient.Dial(ctx, s.url(), wsclient.Options{}); err == nil {
		t.Fatal("Dial with a canceled context = nil error, want one")
	}
}

func TestInvalidCloseCode(t *testing.T) {
	s := newTestServer(t, nil, echo)
	defer s.close()
	c := dial(t, s.url(), wsclient.Options{})
	defer func() { _ = c.Close(0, "") }()
	s.request()

	if err := c.Close(wsclient.CloseAbnormal, ""); !errors.Is(err, wsclient.ErrInvalidCloseCode) {
		t.Fatalf("Close(1006) = %v, want ErrInvalidCloseCode", err)
	}
	if err := c.Close(2999, ""); !errors.Is(err, wsclient.ErrInvalidCloseCode) {
		t.Fatalf("Close(2999) = %v, want ErrInvalidCloseCode", err)
	}
	if c.State() != wsclient.Open {
		t.Fatalf("a refused Close changed State to %v", c.State())
	}
}

func TestAbnormalDropReported(t *testing.T) {
	s := newTestServer(t, nil, func(conn net.Conn, br *bufio.Reader) {
		_ = conn.Close() // no close frame: the network vanished
	})
	defer s.close()
	c := dial(t, s.url(), wsclient.Options{})
	s.request()

	waitState(t, c, wsclient.Closed)
	if code, _ := c.CloseStatus(); code != wsclient.CloseAbnormal {
		t.Fatalf("CloseStatus code = %d, want 1006", code)
	}
	if c.Err() == nil {
		t.Fatal("Err = nil after an abnormal drop, want the read error")
	}
}
