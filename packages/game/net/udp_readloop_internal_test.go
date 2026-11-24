//go:build !js

package net

import (
	"encoding/binary"
	"errors"
	stdnet "net"
	"os"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
)

// scriptedRead is one result a scriptedPacketConn returns from ReadFrom.
type scriptedRead struct {
	data []byte
	from stdnet.Addr
	err  error
}

// scriptedPacketConn returns the reads a test feeds it, one at a time, so a test controls exactly what a receive loop sees.
type scriptedPacketConn struct {
	reads     chan scriptedRead
	closed    chan struct{}
	closeOnce sync.Once
}

func newScriptedPacketConn() *scriptedPacketConn {
	return &scriptedPacketConn{reads: make(chan scriptedRead), closed: make(chan struct{})}
}

func (c *scriptedPacketConn) ReadFrom(b []byte) (int, stdnet.Addr, error) {
	select {
	case r := <-c.reads:
		return copy(b, r.data), r.from, r.err
	case <-c.closed:
		return 0, nil, stdnet.ErrClosed
	}
}

func (c *scriptedPacketConn) WriteTo(b []byte, _ stdnet.Addr) (int, error) { return len(b), nil }
func (c *scriptedPacketConn) LocalAddr() stdnet.Addr {
	return &stdnet.UDPAddr{IP: stdnet.IPv4(127, 0, 0, 1), Port: 1}
}
func (c *scriptedPacketConn) SetDeadline(time.Time) error      { return nil }
func (c *scriptedPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (c *scriptedPacketConn) SetWriteDeadline(time.Time) error { return nil }
func (c *scriptedPacketConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

// feed hands the receive loop one read, failing if the loop has stopped reading.
func (c *scriptedPacketConn) feed(t *testing.T, r scriptedRead) {
	t.Helper()
	select {
	case c.reads <- r:
	case <-time.After(5 * time.Second):
		t.Fatal("the receive loop stopped reading")
	}
}

// icmpRefused is the error a system reports on a UDP socket when an ICMP port unreachable arrives for an earlier send.
func icmpRefused() error {
	return &stdnet.OpError{Op: "read", Net: "udp", Err: os.NewSyscallError("recvfrom", syscall.ECONNREFUSED)}
}

func socketBroken() error {
	return &stdnet.OpError{Op: "read", Net: "udp", Err: os.NewSyscallError("recvfrom", syscall.EBADF)}
}

// waitFor spins, yielding to other goroutines, until cond holds; the timeout only bounds a broken test.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		runtime.Gosched()
	}
}

// #546: UDPTransport's receive loop returned on the first read error. An ICMP port unreachable for an earlier send, which is about one datagram, stopped it for good, and a real socket failure stopped it without any error reaching the game.
func TestUDPTransportReadLoopSurvivesDatagramErrorsAndReportsSocketFailure(t *testing.T) {
	conn := newScriptedPacketConn()
	u := newUDPTransport(conn, 1)
	defer func() { _ = u.Close() }()
	peer := &stdnet.UDPAddr{IP: stdnet.IPv4(127, 0, 0, 1), Port: 9000}
	if err := u.AddPeer(2, peer.String()); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	conn.feed(t, scriptedRead{err: icmpRefused()})
	dgram := make([]byte, udpHeaderSize, udpHeaderSize+2)
	binary.BigEndian.PutUint32(dgram[0:4], 2)
	dgram[4] = byte(ChannelUnreliable)
	binary.BigEndian.PutUint32(dgram[5:9], 1)
	dgram = append(dgram, "hi"...)
	conn.feed(t, scriptedRead{data: dgram, from: peer})
	var pkt Packet
	waitFor(t, "the datagram after the ICMP error", func() bool {
		var ok bool
		pkt, ok = u.Recv()
		return ok
	})
	if pkt.From != 2 || string(pkt.Payload) != "hi" {
		t.Fatalf("packet = %+v", pkt)
	}
	if err := u.Err(); err != nil {
		t.Fatalf("Err after an ICMP error = %v, want nil", err)
	}

	conn.feed(t, scriptedRead{err: socketBroken()})
	waitFor(t, "the socket failure to be reported", func() bool { return u.Err() != nil })
	if err := u.Err(); !errors.Is(err, ErrReceiveFailed) || !errors.Is(err, syscall.EBADF) {
		t.Fatalf("Err = %v, want ErrReceiveFailed wrapping the socket error", err)
	}
	if err := u.Send(2, ChannelUnreliable, []byte("x")); !errors.Is(err, ErrReceiveFailed) {
		t.Fatalf("Send after the socket failed = %v, want ErrReceiveFailed", err)
	}
}

func TestUDPTransportCloseIsNotAReceiveFailure(t *testing.T) {
	conn := newScriptedPacketConn()
	u := newUDPTransport(conn, 1)
	if err := u.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := u.Err(); err != nil {
		t.Fatalf("Err after Close = %v, want nil", err)
	}
}

// #546: the same silent stop in UDPServer left every client to time out with nothing telling the game why.
func TestUDPServerReadLoopSurvivesDatagramErrorsAndReportsSocketFailure(t *testing.T) {
	conn := newScriptedPacketConn()
	s := newUDPServer(conn, 1, Auth{})
	defer func() { _ = s.Close() }()
	from := &stdnet.UDPAddr{IP: stdnet.IPv4(127, 0, 0, 1), Port: 9000}

	conn.feed(t, scriptedRead{err: icmpRefused()})
	// The loop is still reading: it takes this junk datagram, and the next read.
	conn.feed(t, scriptedRead{data: []byte{0xff}, from: from})
	if err := s.Err(); err != nil {
		t.Fatalf("Err after an ICMP error = %v, want nil", err)
	}

	conn.feed(t, scriptedRead{err: socketBroken()})
	awaitChange(t, s.changed, "the socket failure to be reported", func() bool { return s.Err() != nil })
	if err := s.Err(); !errors.Is(err, ErrReceiveFailed) {
		t.Fatalf("Err = %v, want ErrReceiveFailed", err)
	}
	if err := s.Send(PeerBroadcast, ChannelUnreliable, []byte("x")); !errors.Is(err, ErrReceiveFailed) {
		t.Fatalf("Send after the socket failed = %v, want ErrReceiveFailed", err)
	}
}
