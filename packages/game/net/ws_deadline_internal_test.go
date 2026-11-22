package net

import (
	"bufio"
	stdnet "net"
	"net/url"
	"testing"
	"time"
)

func TestWebSocketCloseUnblocksSend(t *testing.T) {
	// #546: Send to a client that never reads used to block forever holding
	// writeMu, so Close could not return either.
	c1, c2 := stdnet.Pipe()
	defer c2.Close()
	wt := &WebSocketTransport{conn: c1, localID: 1, serverID: 2, br: bufio.NewReader(c1)}
	sendErr := make(chan error, 1)
	go func() { sendErr <- wt.Send(2, ChannelReliableOrdered, []byte("hello")) }()
	time.Sleep(20 * time.Millisecond)

	closed := make(chan error, 1)
	go func() { closed <- wt.Close() }()
	select {
	case err := <-sendErr:
		if err == nil {
			t.Fatal("Send to a non-reading client returned nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Send stayed blocked after Close")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close stayed blocked behind Send")
	}
}

func TestWebSocketHostPortHandlesIPv6AndDefaultPorts(t *testing.T) {
	// #546: a colon test mistook an IPv6 literal for an existing port.
	for _, tc := range []struct{ raw, want string }{
		{"ws://[::1]/app", "[::1]:80"},
		{"ws://[::1]:8080/app", "[::1]:8080"},
		{"wss://example.com/app", "example.com:443"},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		got, err := webSocketHostPort(u)
		if err != nil || got != tc.want {
			t.Fatalf("%s host = %q, %v; want %q", tc.raw, got, err, tc.want)
		}
	}
}
