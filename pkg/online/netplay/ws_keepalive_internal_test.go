//go:build !js

package netplay

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	stdnet "net"
	"net/http"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// listenWSKeepAlive starts a server whose keepalive runs at test speed.
func listenWSKeepAlive(t *testing.T, auth Auth, ka wsKeepAlive) *WebSocketServer {
	t.Helper()
	srv, err := listenWebSocket("127.0.0.1:0", 1, auth, nil, nil, ka)
	if err != nil {
		t.Fatalf("listenWebSocket: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// #546: a client whose network vanished without a FIN or RST never sends another byte. The server had no keepalive, so it kept that session, and its ID, forever.
func TestWebSocketServerDropsASilentClient(t *testing.T) {
	srv := listenWSKeepAlive(t, Auth{}, wsKeepAlive{ping: 20 * time.Millisecond, idle: 150 * time.Millisecond})
	h, _ := newHello([]byte("token"), resumeState{})
	_, _, w, err := rawWSHello(t, srv, h)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerJoined, w.peer}) {
		t.Fatalf("event = %+v, want joined", ev)
	}
	// The raw client never reads, so it never answers the server's pings.
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerLeft, w.peer}) {
		t.Fatalf("event = %+v, want the silent client to leave (no grace period)", ev)
	}
	if got := srv.Peers(); len(got) != 0 {
		t.Fatalf("Peers = %v after the silent client dropped", got)
	}
}

func TestWebSocketServerKeepsAClientThatAnswersPings(t *testing.T) {
	srv := listenWSKeepAlive(t, Auth{}, wsKeepAlive{ping: 25 * time.Millisecond, idle: 500 * time.Millisecond})
	h, _ := newHello([]byte("token"), resumeState{})
	conn, br, w, err := rawWSHello(t, srv, h)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerJoined, w.peer}) {
		t.Fatalf("event = %+v, want joined", ev)
	}
	// Answer pings, and nothing else, for longer than the idle timeout.
	for pings := 0; pings < 30; {
		_, op, payload, _, err := readWSFrame(br)
		if err != nil {
			t.Fatalf("read after %d pings: %v", pings, err)
		}
		if op != wsOpPing {
			t.Fatalf("server sent opcode %#x, want only pings", op)
		}
		if err := writeWSFrame(conn, wsOpPong, true, payload); err != nil {
			t.Fatalf("pong: %v", err)
		}
		pings++
	}
	expectNoEvent(t, srv)
	if got := srv.Peers(); len(got) != 1 || got[0] != w.peer {
		t.Fatalf("Peers = %v, want the client that answers pings", got)
	}
}

// silentWSServer accepts one WebSocket client, completes the upgrade and the connection handshake, and then never sends another byte or reads one.
func silentWSServer(t *testing.T) string {
	t.Helper()
	url, _ := fakeWSServer(t, 0)
	return url
}

// fakeWSServer accepts one WebSocket client and completes the upgrade and the connection handshake. It then answers the client's pings, closing pinged after the given number of them, or with zero stays silent and reads nothing.
func fakeWSServer(t *testing.T, answer int) (string, <-chan struct{}) {
	t.Helper()
	pinged := make(chan struct{})
	ln, err := stdnet.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		br := bufio.NewReader(conn)
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + wsAcceptKey(req.Header.Get("Sec-WebSocket-Key")) + "\r\n\r\n"
		if _, err := io.WriteString(conn, resp); err != nil {
			return
		}
		_, _, payload, _, err := readWSFrame(br)
		if err != nil {
			return
		}
		h, err := decodeHello(payload)
		if err != nil {
			return
		}
		if err := writeWSFrame(conn, wsOpBinary, false, encodeWelcome(welcome{nonce: h.nonce, peer: 2, server: 1})); err != nil {
			return
		}
		for pings := 0; pings < answer; {
			_, op, payload, _, err := readWSFrame(br)
			if err != nil {
				return
			}
			if op != wsOpPing {
				continue
			}
			if err := writeWSFrame(conn, wsOpPong, false, payload); err != nil {
				return
			}
			if pings++; pings == answer {
				close(pinged)
			}
		}
		// Hold the connection open, silent, until the test ends.
		_, _ = ln.Accept()
	}()
	return "ws://" + ln.Addr().String() + "/game", pinged
}

// #546: the native client had no read deadline either, so a server that vanished left it ConnOpen forever.
func TestWebSocketClientDetectsASilentServer(t *testing.T) {
	url := silentWSServer(t)
	wt, err := dialWebSocket(url, []byte("token"), wsKeepAlive{ping: 20 * time.Millisecond, idle: 150 * time.Millisecond})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = wt.Close() }()
	if wt.State() != ConnOpen || wt.LocalID() != 2 {
		t.Fatalf("after dial state = %v, id = %d", wt.State(), wt.LocalID())
	}
	awaitChange(t, wt.changed, "the client to notice the silent server", func() bool { return wt.State() == ConnDisconnected })
	if err := wt.Err(); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("Err = %v, want ErrDisconnected", err)
	}
}

func TestWebSocketClientStaysOpenWhileTheServerAnswersPings(t *testing.T) {
	url, pinged := fakeWSServer(t, 30)
	wt, err := dialWebSocket(url, []byte("token"), wsKeepAlive{ping: 25 * time.Millisecond, idle: 400 * time.Millisecond})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = wt.Close() }()
	// The server answers 30 pings, 750 ms of pinging, and sends nothing else; the client must stay open through more than its idle timeout.
	select {
	case <-pinged:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the client's pings")
	}
	if wt.State() != ConnOpen {
		t.Fatalf("client state = %v (%v) while the server answered its pings", wt.State(), wt.Err())
	}
}

// #546: readWSFrame allocated the length a frame announced before any of its payload arrived, so a peer could reserve 4 MiB per connection with a 10-byte header and then trickle the rest (slow loris).
func TestReadWSFrameAllocatesOnlyWhatArrives(t *testing.T) {
	var frame bytes.Buffer
	frame.Write([]byte{0x82, 127})
	_ = binary.Write(&frame, binary.BigEndian, uint64(maxWSMessageSize))
	frame.Write(make([]byte, 1000)) // then the connection ends

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, _, _, _, err := readWSFrame(bufio.NewReader(&frame))
	runtime.ReadMemStats(&after)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v, want io.ErrUnexpectedEOF for a truncated frame", err)
	}
	if got := after.TotalAlloc - before.TotalAlloc; got > 1<<20 {
		t.Fatalf("reading 1000 bytes of a frame announced as %d bytes allocated %d bytes", maxWSMessageSize, got)
	}
}

func TestReadWSFrameReadsLargeFramesWhole(t *testing.T) {
	want := bytes.Repeat([]byte("0123456789abcdef"), (wsEagerAlloc*3)/16+1)
	var frame bytes.Buffer
	if err := writeWSFrame(&frame, wsOpBinary, true, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	fin, op, got, masked, err := readWSFrame(bufio.NewReader(&frame))
	if err != nil || !fin || op != wsOpBinary || !masked || !bytes.Equal(got, want) {
		t.Fatalf("read = fin %v op %#x masked %v len %d, %v; want the %d-byte payload", fin, op, masked, len(got), err, len(want))
	}
}

func TestWebSocketServerRefusesAnOversizedHello(t *testing.T) {
	srv := listenWSKeepAlive(t, Auth{}, serverKeepAlive(Auth{}))
	conn, br := rawUpgrade(t, srv)
	defer func() { _ = conn.Close() }()
	// A hello frame longer than any valid hello is refused from its header, before its payload is read.
	var hdr bytes.Buffer
	hdr.Write([]byte{0x82, 0x80 | 127})
	_ = binary.Write(&hdr, binary.BigEndian, uint64(wsMaxHelloFrame+1))
	hdr.Write([]byte{0, 0, 0, 0})
	if _, err := conn.Write(hdr.Bytes()); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := br.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("read after an oversized hello = %v, want the server to close the connection", err)
	}
}

// #546 review: the idle deadline was set once per frame, so a large frame that took longer than the idle timeout to arrive dropped the connection mid-frame even though its bytes kept coming.
func TestWebSocketServerReadsAFrameThatArrivesSlowlyButSteadily(t *testing.T) {
	srv := listenWSKeepAlive(t, Auth{}, wsKeepAlive{ping: 20 * time.Millisecond, idle: 150 * time.Millisecond})
	h, _ := newHello([]byte("token"), resumeState{})
	conn, br, w, err := rawWSHello(t, srv, h)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerJoined, w.peer}) {
		t.Fatalf("event = %+v, want joined", ev)
	}

	want := bytes.Repeat([]byte("slow"), 4096)
	var frame bytes.Buffer
	if err := writeWSFrame(&frame, wsOpBinary, true, encodeWireFrame(ChannelReliableOrdered, 1, want)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Send one slice of the frame per server ping: 24 pings, about 480 ms, more than three idle timeouts, with bytes arriving every 20 ms.
	const parts = 24
	raw := frame.Bytes()
	step := (len(raw) + parts - 1) / parts
	for off := 0; off < len(raw); off += step {
		_, op, _, _, err := readWSFrame(br)
		if err != nil {
			t.Fatalf("read at offset %d: %v", off, err)
		}
		if op != wsOpPing {
			t.Fatalf("server sent opcode %#x, want a ping", op)
		}
		if _, err := conn.Write(raw[off:min(off+step, len(raw))]); err != nil {
			t.Fatalf("write at offset %d: %v", off, err)
		}
	}

	var pkt Packet
	awaitChange(t, srv.changed, "the slow frame", func() bool {
		var ok bool
		pkt, ok = srv.Recv()
		return ok
	})
	if pkt.From != w.peer || !bytes.Equal(pkt.Payload, want) {
		t.Fatalf("packet from %d with %d bytes, want %d bytes from %d", pkt.From, len(pkt.Payload), len(want), w.peer)
	}
	expectNoEvent(t, srv)
}

// #546 review: Close took each client's write lock in turn without breaking a Send already blocked on a client that does not read, so it waited out that Send's 10-second write deadline, once per such client.
func TestWebSocketServerCloseBreaksABlockedSend(t *testing.T) {
	srv, err := listenWebSocket("127.0.0.1:0", 1, Auth{}, nil, nil, serverKeepAlive(Auth{}))
	if err != nil {
		t.Fatalf("listenWebSocket: %v", err)
	}
	var clients []PeerID
	for range 2 {
		h, _ := newHello([]byte("token"), resumeState{})
		if _, _, w, err := rawWSHello(t, srv, h); err != nil {
			t.Fatalf("handshake: %v", err)
		} else {
			clients = append(clients, w.peer)
		}
	}
	// The raw clients never read, so a Send of 4 MiB frames blocks on a full socket buffer once loopback's buffers are full, within the first frame or two.
	big := make([]byte, maxWSMessageSize-wireHeaderSize)
	var started atomic.Int32
	done := make(chan struct{}, len(clients))
	for _, id := range clients {
		go func() {
			defer func() { done <- struct{}{} }()
			for {
				started.Add(1)
				if err := srv.Send(id, ChannelReliableOrdered, big); err != nil {
					return
				}
			}
		}()
	}
	waitFor(t, "every sender to start sending", func() bool { return started.Load() >= int32(len(clients)) })

	begin := time.Now()
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if took := time.Since(begin); took > 5*time.Second {
		t.Fatalf("Close took %v with Sends blocked on clients that do not read", took)
	}
	for range clients {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("a blocked Send did not return after Close")
		}
	}
}

// rawWSControlFrame builds a raw RFC 6455 frame with exact FIN, opcode, and length fields.
func rawWSControlFrame(fin bool, opcode byte, masked bool, payload []byte) []byte {
	var b bytes.Buffer
	first := opcode & 0x0F
	if fin {
		first |= 0x80
	}
	b.WriteByte(first)
	var maskBit byte
	if masked {
		maskBit = 0x80
	}
	switch n := len(payload); {
	case n < 126:
		b.WriteByte(maskBit | byte(n))
	case n <= 0xFFFF:
		b.WriteByte(maskBit | 126)
		_ = binary.Write(&b, binary.BigEndian, uint16(n))
	default:
		b.WriteByte(maskBit | 127)
		_ = binary.Write(&b, binary.BigEndian, uint64(n))
	}
	if masked {
		maskKey := [4]byte{0x12, 0x34, 0x56, 0x78}
		b.Write(maskKey[:])
		for i, v := range payload {
			b.WriteByte(v ^ maskKey[i&3])
		}
	} else {
		b.Write(payload)
	}
	return b.Bytes()
}

// TestReadWSFrameControlFrameChecks verifies RFC 6455 §5.5 control-frame rules:
// control frames (opcode >= 0x8) must be unfragmented (FIN=1), carry at most
// 125 payload bytes, and use only defined opcodes 0x8..0xA (rejecting 0xB..0xF).
func TestReadWSFrameControlFrameChecks(t *testing.T) {
	ping125 := bytes.Repeat([]byte{'p'}, 125)
	fin, op, got, masked, err := readWSFrame(bufio.NewReader(bytes.NewReader(rawWSControlFrame(true, wsOpPing, true, ping125))))
	if err != nil || !fin || op != wsOpPing || !masked || !bytes.Equal(got, ping125) {
		t.Fatalf("125-byte ping: fin=%v op=%#x masked=%v len=%d err=%v", fin, op, masked, len(got), err)
	}

	// Valid 125-byte close and pong are accepted too.
	for _, validOp := range []byte{wsOpClose, wsOpPong} {
		fin, op, got, _, err := readWSFrame(bufio.NewReader(bytes.NewReader(rawWSControlFrame(true, validOp, false, ping125))))
		if err != nil || !fin || op != validOp || !bytes.Equal(got, ping125) {
			t.Fatalf("125-byte opcode %#x: fin=%v op=%#x len=%d err=%v", validOp, fin, op, len(got), err)
		}
	}

	over126 := bytes.Repeat([]byte{'x'}, 126)
	badCases := []struct {
		name  string
		frame []byte
	}{
		{"126-byte close", rawWSControlFrame(true, wsOpClose, true, over126)},
		{"126-byte pong", rawWSControlFrame(true, wsOpPong, true, over126)},
		{"126-byte ping", rawWSControlFrame(true, wsOpPing, true, over126)},
		{"fragmented ping", rawWSControlFrame(false, wsOpPing, true, []byte("hi"))},
		{"fragmented close", rawWSControlFrame(false, wsOpClose, true, nil)},
		{"fragmented pong", rawWSControlFrame(false, wsOpPong, true, nil)},
		{"reserved 0xB", rawWSControlFrame(true, 0xB, true, nil)},
		{"reserved 0xC", rawWSControlFrame(true, 0xC, true, nil)},
		{"reserved 0xD", rawWSControlFrame(true, 0xD, true, nil)},
		{"reserved 0xE", rawWSControlFrame(true, 0xE, true, nil)},
		{"reserved 0xF", rawWSControlFrame(true, 0xF, true, nil)},
	}
	for _, tc := range badCases {
		if _, _, _, _, err := readWSFrame(bufio.NewReader(bytes.NewReader(tc.frame))); !errors.Is(err, ErrInvalidPacket) {
			t.Errorf("%s: err = %v, want ErrInvalidPacket", tc.name, err)
		}
	}

	// End-to-end check on WebSocketServer: a 125-byte ping gets a matching pong,
	// and a fragmented ping closes the connection.
	srv := listenWSKeepAlive(t, Auth{}, wsKeepAlive{ping: time.Minute, idle: 5 * time.Second})
	h, _ := newHello([]byte("token"), resumeState{})
	conn, br, w, err := rawWSHello(t, srv, h)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerJoined, w.peer}) {
		t.Fatalf("event = %+v, want joined", ev)
	}
	if _, err := conn.Write(rawWSControlFrame(true, wsOpPing, true, ping125)); err != nil {
		t.Fatalf("write 125-byte ping: %v", err)
	}
	fin, op, got, _, err = readWSFrame(br)
	if err != nil || !fin || op != wsOpPong || !bytes.Equal(got, ping125) {
		t.Fatalf("pong reply: fin=%v op=%#x len=%d err=%v", fin, op, len(got), err)
	}
	if _, err := conn.Write(rawWSControlFrame(false, wsOpPing, true, []byte("frag"))); err != nil {
		t.Fatalf("write fragmented ping: %v", err)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{PeerLeft, w.peer}) {
		t.Fatalf("event after fragmented ping = %+v, want left", ev)
	}
}
