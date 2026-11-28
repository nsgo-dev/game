//go:build !js

package netplay

import (
	"bufio"
	"context"
	"encoding/binary"
	stdnet "net"
	"net/url"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/webtransport-go"
)

// wsTestFrame is a binary frame a raw test client read from the server.
type wsTestFrame struct {
	ch  Channel
	seq uint32
}

// readWSClientFrames reads the server's frames on a raw client, answers its pings, and passes every binary frame's channel and sequence number to frames until the connection ends.
func readWSClientFrames(conn stdnet.Conn, br *bufio.Reader, frames chan<- wsTestFrame) {
	for {
		_, op, payload, _, err := readWSFrame(br)
		if err != nil {
			close(frames)
			return
		}
		switch op {
		case wsOpPing:
			_ = writeWSFrame(conn, wsOpPong, true, payload)
		case wsOpBinary:
			ch, seq, _, err := decodeWireFrame(payload)
			if err != nil {
				close(frames)
				return
			}
			frames <- wsTestFrame{ch: ch, seq: seq}
		}
	}
}

// A client that completes the handshake and then stops reading filled its socket buffers, and the next Send to it blocked the caller, the match's tick, for the 10-second write timeout, so every other client stopped receiving too.
func TestWebSocketServerSendDoesNotBlockOnAClientThatStopsReading(t *testing.T) {
	srv := listenWSKeepAlive(t, Auth{}, wsKeepAlive{ping: time.Second, idle: time.Minute})

	h, _ := newHello([]byte("token"), resumeState{})
	_, _, stalled, err := rawWSHello(t, srv, h)
	if err != nil {
		t.Fatalf("stalled client handshake: %v", err)
	}
	h, _ = newHello([]byte("token"), resumeState{})
	conn, br, healthy, err := rawWSHello(t, srv, h)
	if err != nil {
		t.Fatalf("healthy client handshake: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(time.Minute))
	frames := make(chan wsTestFrame, 16)
	go readWSClientFrames(conn, br, frames)

	payload := make([]byte, 64<<10)
	const rounds = 1024 // 64 MiB to each client, far past any loopback socket buffer
	stalledGone := false
	for i := range rounds {
		sent := make(chan error, 1)
		go func() { sent <- srv.Send(PeerBroadcast, ChannelReliableOrdered, payload) }()
		select {
		case <-sent:
		case <-time.After(2 * time.Second):
			t.Fatalf("broadcast %d blocked for more than 2s on a client that stopped reading", i+1)
		}
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatalf("healthy client's connection ended after %d broadcasts", i)
			}
			if f.ch != ChannelReliableOrdered || f.seq != uint32(i+1) {
				t.Fatalf("healthy client got %+v, want reliable seq %d", f, i+1)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("healthy client did not receive broadcast %d", i+1)
		}
		if peers := srv.Peers(); len(peers) == 1 && peers[0] == healthy.peer {
			stalledGone = true
			break
		}
	}
	if !stalledGone {
		t.Fatalf("Peers = %v after %d broadcasts, want the client that stopped reading gone", srv.Peers(), rounds)
	}
	for _, want := range []PeerEvent{{Kind: PeerJoined, Peer: stalled.peer}, {Kind: PeerJoined, Peer: healthy.peer}, {Kind: PeerLeft, Peer: stalled.peer, Reason: reasonOutboxFull}} {
		if ev := nextEvent(t, srv, srv.changed); ev != want {
			t.Fatalf("event = %+v, want %+v", ev, want)
		}
	}
	if st := srv.OutboxStats(); st.Overflows != 1 {
		t.Fatalf("OutboxStats = %+v, want one session ended for overflow", st)
	}
}

// A write that timed out could leave part of a frame on the wire, and the connection stayed open: later frames continued from the middle of that frame, so the client misparsed every one of them.
func TestWebSocketServerClosesAClientWhoseWriteTimedOut(t *testing.T) {
	// A grace period tells the drop a timed-out write causes apart from the leave an outbox overflow causes.
	srv := listenWSKeepAlive(t, Auth{GracePeriod: time.Minute}, wsKeepAlive{ping: time.Second, idle: time.Minute})
	srv.mu.Lock()
	srv.writeTimeout = 200 * time.Millisecond
	srv.mu.Unlock()
	srv.SetOutboxLimits(OutboxLimits{Frames: -1, Bytes: -1})

	h, _ := newHello([]byte("token"), resumeState{})
	conn, br, w, err := rawWSHello(t, srv, h)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerJoined, Peer: w.peer}) {
		t.Fatalf("event = %+v, want joined", ev)
	}

	// Each frame's payload repeats one byte taken from its sequence number, so a frame read from the middle of another one shows.
	sent := 0
	for len(srv.Peers()) == 1 && sent < 4096 {
		sent++
		payload := make([]byte, 64<<10)
		for i := range payload {
			payload[i] = byte(sent)
		}
		_ = srv.Send(w.peer, ChannelReliableOrdered, payload)
		if sent%16 == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitFor(t, "the client whose write timed out to drop", func() bool { return len(srv.Peers()) == 0 })
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerDropped, Peer: w.peer}) {
		t.Fatalf("event = %+v, want dropped", ev)
	}

	// The client now reads everything the server wrote: whole frames in order, then the end of the connection, perhaps after the start of the frame that timed out.
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	want := uint32(1)
	for {
		_, op, payload, _, err := readWSFrame(br)
		if err != nil {
			if ne, ok := err.(stdnet.Error); ok && ne.Timeout() {
				t.Fatalf("connection still open after the write timed out (read %d frames)", want-1)
			}
			break
		}
		if op != wsOpBinary {
			continue
		}
		if len(payload) != wireHeaderSize+64<<10 {
			t.Fatalf("frame %d is %d bytes, want %d", want, len(payload), wireHeaderSize+64<<10)
		}
		if seq := binary.BigEndian.Uint32(payload[1:5]); seq != want {
			t.Fatalf("frame seq = %d, want %d", seq, want)
		}
		for _, b := range payload[wireHeaderSize:] {
			if b != byte(want) {
				t.Fatalf("frame %d carries bytes of another frame: the stream is misframed", want)
			}
		}
		want++
	}
}

// stalledWTClient opens a WebTransport session to srv, completes the connection handshake on the reliable stream, and then never reads the stream again, so the server's stream writes block once QUIC's flow-control window is used up.
func stalledWTClient(t *testing.T, rawURL string, opts WebTransportOptions) welcome {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	tr := &webtransport.Transport{
		TLSClientConfig: buildClientTLSConfig(u.Hostname(), opts.ServerCertificateHashes),
		QUICConfig:      &quic.Config{EnableDatagrams: true, EnableStreamResetPartialDelivery: true, KeepAlivePeriod: time.Second},
	}
	t.Cleanup(func() { _ = tr.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, sess, err := tr.Dial(ctx, rawURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	stream, err := sess.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	w, err := wtClientHandshake(stream, []byte("token"), resumeState{})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	return w
}

// The WebTransport server wrote reliable frames to each client's stream on the caller's goroutine, so a client that stopped reading its stream blocked Send, and every other client, once QUIC's flow-control window was full.
func TestWebTransportServerSendDoesNotBlockOnAClientThatStopsReading(t *testing.T) {
	srv, rawURL, opts := listenWT(t, Auth{}, nil)
	stalled := stalledWTClient(t, rawURL, opts)
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerJoined, Peer: stalled.peer}) {
		t.Fatalf("event = %+v, want the stalled client joined", ev)
	}
	healthy, err := DialWebTransport(rawURL, nil, opts)
	if err != nil {
		t.Fatalf("DialWebTransport: %v", err)
	}
	t.Cleanup(func() { _ = healthy.Close() })
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerJoined, Peer: healthy.LocalID()}) {
		t.Fatalf("event = %+v, want the healthy client joined", ev)
	}

	payload := make([]byte, 64<<10)
	const rounds = 1024
	stalledGone := false
	for i := range rounds {
		sent := make(chan error, 1)
		go func() { sent <- srv.Send(PeerBroadcast, ChannelReliableOrdered, payload) }()
		select {
		case <-sent:
		case <-time.After(2 * time.Second):
			t.Fatalf("broadcast %d blocked for more than 2s on a client that stopped reading", i+1)
		}
		if pkt := recvOn(t, healthy, healthy.changed); pkt.Channel != ChannelReliableOrdered || pkt.Seq != uint32(i+1) {
			t.Fatalf("healthy client got %v seq %d, want reliable seq %d", pkt.Channel, pkt.Seq, i+1)
		}
		if peers := srv.Peers(); len(peers) == 1 && peers[0] == healthy.LocalID() {
			stalledGone = true
			break
		}
	}
	if !stalledGone {
		t.Fatalf("Peers = %v after %d broadcasts, want the client that stopped reading gone", srv.Peers(), rounds)
	}
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerLeft, Peer: stalled.peer, Reason: reasonOutboxFull}) {
		t.Fatalf("event = %+v, want the stalled client left with %q", ev, reasonOutboxFull)
	}
	if st := srv.OutboxStats(); st.Overflows != 1 {
		t.Fatalf("OutboxStats = %+v, want one session ended for overflow", st)
	}
}

func TestOutboxDropsOldestUnreliableAndRefusesReliable(t *testing.T) {
	var counters outboxCounters
	o := newOutbox(&counters)
	limits := OutboxLimits{Frames: 3, Bytes: -1}
	push := func(ch Channel, tag byte) pushResult {
		return o.push(ch, []byte{tag}, limits, encodeWireFrame)
	}
	for _, step := range []struct {
		ch   Channel
		tag  byte
		want pushResult
	}{
		{ChannelUnreliable, 'a', pushQueued},
		{ChannelReliableOrdered, 'B', pushQueued},
		{ChannelUnreliable, 'c', pushQueued},
		{ChannelUnreliable, 'd', pushQueued},      // drops a
		{ChannelReliableOrdered, 'E', pushQueued}, // drops c
		{ChannelReliableOrdered, 'F', pushQueued}, // drops d
		{ChannelUnreliable, 'g', pushDropped},     // no unreliable frame left to drop
		{ChannelReliableOrdered, 'H', pushOverflow},
	} {
		if got := push(step.ch, step.tag); got != step.want {
			t.Fatalf("push %c = %d, want %d", step.tag, got, step.want)
		}
	}
	if got := counters.dropped.Load(); got != 4 {
		t.Fatalf("dropped = %d, want 4", got)
	}
	var tags []byte
	for range 3 {
		frame, ok := o.next()
		if !ok {
			t.Fatal("next reported closed with frames queued")
		}
		tags = append(tags, frame[wireHeaderSize])
	}
	if string(tags) != "BEF" {
		t.Fatalf("written frames = %q, want the reliable frames in order", tags)
	}
	if frames, bytes := o.stats(); frames != 0 || bytes != 0 {
		t.Fatalf("stats = %d frames, %d bytes after draining", frames, bytes)
	}

	// An empty outbox takes a frame of any size, and a byte cap drops unreliable frames to make room.
	limits = OutboxLimits{Frames: -1, Bytes: 2 * (wireHeaderSize + 1)}
	if got := o.push(ChannelReliableOrdered, make([]byte, 100), limits, encodeWireFrame); got != pushQueued {
		t.Fatalf("push into an empty outbox = %d, want queued", got)
	}
	_, _ = o.next()
	push(ChannelUnreliable, 'x')
	push(ChannelUnreliable, 'y')
	if got := push(ChannelReliableOrdered, 'Z'); got != pushQueued {
		t.Fatalf("push Z = %d, want queued after dropping x", got)
	}
	if frames, bytes := o.stats(); frames != 2 || bytes != 2*(wireHeaderSize+1) {
		t.Fatalf("stats = %d frames, %d bytes, want 2 frames", frames, bytes)
	}

	o.close()
	if _, ok := o.next(); ok {
		t.Fatal("next returned a frame after close")
	}
	if got := push(ChannelReliableOrdered, 'Q'); got != pushClosed {
		t.Fatalf("push after close = %d, want closed", got)
	}
}
