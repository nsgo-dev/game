package netplay

import (
	"net"
	"testing"
)

func TestServeWebSocketUsesInheritedListenerAndStopsAccepting(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ServeWebSocket(ln, 1, Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Addr().String() != ln.Addr().String() {
		t.Fatalf("addr = %s, want %s", s.Addr(), ln.Addr())
	}
	if err := s.StopAccepting(); err != nil {
		t.Fatal(err)
	}
	if _, err := ln.Accept(); err == nil {
		t.Fatal("Accept succeeded after StopAccepting")
	}
	_ = s.Close()
}
