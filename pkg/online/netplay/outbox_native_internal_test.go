//go:build !js

package netplay

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	stdnet "net"
	"net/url"
	"sync/atomic"
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
		item, ok := o.next()
		if !ok || item.final {
			t.Fatalf("next = %+v, %v with frames queued", item, ok)
		}
		tags = append(tags, item.data[wireHeaderSize])
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

func TestOutboxFinishDrainsTheQueueThenTheFinalFrame(t *testing.T) {
	var counters outboxCounters
	o := newOutbox(&counters)
	limits := OutboxLimits{}
	for _, tag := range []byte("ab") {
		if got := o.push(ChannelReliableOrdered, []byte{tag}, limits, encodeWireFrame); got != pushQueued {
			t.Fatalf("push %c = %d, want queued", tag, got)
		}
	}
	by := time.Now().Add(time.Minute)
	if !o.finish([]byte("bye"), by) {
		t.Fatal("finish on an open outbox reported false")
	}
	if !o.finish([]byte("other"), by.Add(time.Hour)) {
		t.Fatal("a second finish reported false before the writer took the final frame")
	}
	if got := o.push(ChannelReliableOrdered, []byte{'c'}, limits, encodeWireFrame); got != pushClosed {
		t.Fatalf("push after finish = %d, want closed", got)
	}
	var tags []byte
	for {
		item, ok := o.next()
		if !ok {
			t.Fatal("next reported closed before the final frame")
		}
		if !item.flushBy.Equal(by) {
			t.Fatalf("flushBy = %v, want the first finish's %v", item.flushBy, by)
		}
		if item.final {
			if string(item.data) != "bye" {
				t.Fatalf("final frame = %q, want the first finish's", item.data)
			}
			break
		}
		tags = append(tags, item.data[wireHeaderSize])
	}
	if string(tags) != "ab" {
		t.Fatalf("frames before the final one = %q, want ab", tags)
	}
	if _, ok := o.next(); ok {
		t.Fatal("next returned an item after the final frame")
	}
	if o.finish(nil, by) {
		t.Fatal("finish on a closed outbox reported true")
	}
}

// wsClientReader reads a raw client's connection to the end, answering pings, and counts the binary frames before the close frame.
type wsClientReader struct {
	done      chan struct{}
	frames    int    // binary frames read in order, set before done closes
	closeCode uint16 // the close frame's code, 0 when none came
	err       string // the first problem with the stream
}

func readWSClientToClose(conn stdnet.Conn, br *bufio.Reader) *wsClientReader {
	r := &wsClientReader{done: make(chan struct{})}
	go func() {
		defer close(r.done)
		for {
			_, op, payload, _, err := readWSFrame(br)
			if err != nil {
				return
			}
			switch op {
			case wsOpPing:
				_ = writeWSFrame(conn, wsOpPong, true, payload)
			case wsOpBinary:
				if _, seq, _, err := decodeWireFrame(payload); err != nil || seq != uint32(r.frames+1) {
					r.err = "binary frames out of order"
					return
				}
				r.frames++
			case wsOpClose:
				r.closeCode = wsCloseCode(payload)
				return
			}
		}
	}()
	return r
}

// Send only queues a frame, so Kick and Close, which used to come after the frames Send wrote, cut the connection while the frames sent just before were still queued: a match's result or a ban notice sent before Kick never arrived.
func TestWebSocketServerKickAndCloseDeliverTheFramesSentBefore(t *testing.T) {
	const sent = 50
	for _, end := range []string{"Kick", "Close"} {
		t.Run(end, func(t *testing.T) {
			srv := listenWSKeepAlive(t, Auth{}, wsKeepAlive{ping: time.Second, idle: time.Minute})
			h, _ := newHello([]byte("token"), resumeState{})
			conn, br, w, err := rawWSHello(t, srv, h)
			if err != nil {
				t.Fatalf("handshake: %v", err)
			}
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			r := readWSClientToClose(conn, br)
			for range sent {
				if err := srv.Send(w.peer, ChannelReliableOrdered, make([]byte, 1024)); err != nil {
					t.Fatalf("Send: %v", err)
				}
			}
			wantCode := uint16(0)
			if end == "Kick" {
				if err := srv.Kick(w.peer, "bye"); err != nil {
					t.Fatalf("Kick: %v", err)
				}
				wantCode = wsCloseKicked
			} else if err := srv.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			<-r.done
			if r.err != "" || r.frames != sent || r.closeCode != wantCode {
				t.Fatalf("client read %d frames (%s) and close code %d, want %d frames and close code %d", r.frames, r.err, r.closeCode, sent, wantCode)
			}
		})
	}
}

// Kick wrote the close frame and closed the connection on the caller's goroutine, so kicking a client that had stopped reading held the match's tick for the close frame's second, and for the TLS close alert's five more seconds on a TLS listener.
func TestWebSocketServerKickDoesNotWaitOnAClientThatStopsReading(t *testing.T) {
	srv := listenWSKeepAlive(t, Auth{}, wsKeepAlive{ping: time.Second, idle: time.Minute})
	srv.SetOutboxLimits(OutboxLimits{Frames: -1, Bytes: -1})
	h, _ := newHello([]byte("token"), resumeState{})
	_, _, w, err := rawWSHello(t, srv, h)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	// Queue far more than the socket buffers hold, so the writer is stuck in a write when Kick comes.
	for range 256 {
		_ = srv.Send(w.peer, ChannelReliableOrdered, make([]byte, 64<<10))
	}
	waitFor(t, "the writer to block on the client", func() bool { return srv.OutboxStats().Frames > 0 })
	if err := srv.Kick(w.peer, "afk"); err != nil {
		t.Fatalf("Kick: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// udpDropProxy forwards one client's QUIC packets to a server and the server's back, until drop is set: from then on it throws the server's packets away, so the client never acknowledges anything the server sends while it keeps sending packets of its own.
type udpDropProxy struct {
	pc   stdnet.PacketConn
	drop atomic.Bool
}

func newUDPDropProxy(t *testing.T, server stdnet.Addr) *udpDropProxy {
	t.Helper()
	pc, err := stdnet.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen proxy: %v", err)
	}
	up, err := stdnet.Dial("udp", server.String())
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close(); _ = up.Close() })
	p := &udpDropProxy{pc: pc}
	var client atomic.Pointer[stdnet.Addr]
	go func() {
		buf := make([]byte, 1<<16)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			client.Store(&from)
			_, _ = up.Write(buf[:n])
		}
	}()
	go func() {
		buf := make([]byte, 1<<16)
		for {
			n, err := up.Read(buf)
			if err != nil {
				return
			}
			if to := client.Load(); to != nil && !p.drop.Load() {
				_, _ = pc.WriteTo(buf[:n], *to)
			}
		}
	}()
	return p
}

// Send handed unreliable frames to QUIC's SendDatagram on the caller's goroutine, which waits while the connection's queue of 32 datagrams is full: a client that kept its connection alive but stopped acknowledging held every broadcast for the retransmission timer, which doubles each time.
func TestWebTransportServerSendDoesNotBlockOnAClientThatStopsAcknowledging(t *testing.T) {
	srv, rawURL, opts := listenWT(t, Auth{}, nil)
	proxy := newUDPDropProxy(t, srv.Addr())
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Host = proxy.pc.LocalAddr().String()
	stalled, err := DialWebTransport(u.String(), nil, opts)
	if err != nil {
		t.Fatalf("dial through the proxy: %v", err)
	}
	t.Cleanup(func() { _ = stalled.Close() })
	if ev := nextEvent(t, srv, srv.changed); ev != (PeerEvent{Kind: PeerJoined, Peer: stalled.LocalID()}) {
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
	proxy.drop.Store(true)

	payload := make([]byte, 1000)
	received := 0
	for i := range 300 {
		sent := make(chan error, 1)
		go func() { sent <- srv.Send(PeerBroadcast, ChannelUnreliable, payload) }()
		select {
		case err := <-sent:
			if err != nil {
				t.Fatalf("broadcast %d: %v", i+1, err)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("broadcast %d blocked for more than 500ms on a client that stopped acknowledging", i+1)
		}
		for {
			if _, ok := healthy.Recv(); !ok {
				break
			}
			received++
		}
		time.Sleep(2 * time.Millisecond)
	}
	waitFor(t, "the healthy client to receive broadcasts", func() bool {
		for {
			if _, ok := healthy.Recv(); !ok {
				break
			}
			received++
		}
		return received > 0
	})

	// A payload past the datagram size is still refused at once.
	if err := srv.Send(healthy.LocalID(), ChannelUnreliable, make([]byte, 8192)); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("oversized datagram err = %v, want ErrPayloadTooLarge", err)
	}
}
