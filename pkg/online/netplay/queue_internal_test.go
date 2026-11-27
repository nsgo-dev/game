package netplay

import (
	"testing"
)

// #546: Recv used to shift the whole inbox down by one on every pop, so draining n queued packets copied n²/2 packets.
func TestPacketQueueDrainIsLinear(t *testing.T) {
	const n = 1 << 14
	var q packetQueue
	for i := range n {
		q.push(Packet{Seq: uint32(i)})
	}
	for i := range n {
		p, ok := q.pop()
		if !ok || p.Seq != uint32(i) {
			t.Fatalf("pop %d = %+v, %v; want seq %d", i, p, ok, i)
		}
		if q.len() != n-i-1 {
			t.Fatalf("len after %d pops = %d, want %d", i+1, q.len(), n-i-1)
		}
	}
	if _, ok := q.pop(); ok {
		t.Fatal("pop on an empty queue succeeded")
	}
	if q.rel.moved > n {
		t.Fatalf("draining %d packets moved %d of them, want at most one move per packet", n, q.rel.moved)
	}
}

func TestPacketQueueInterleavedPushPopStaysFIFOAndLinear(t *testing.T) {
	var q packetQueue
	next, want := uint32(0), uint32(0)
	const rounds = 1 << 12
	for range rounds {
		// Push three, pop two: the queue keeps growing while its front is consumed, which is when compaction runs.
		for range 3 {
			q.push(Packet{Seq: next})
			next++
		}
		for range 2 {
			p, ok := q.pop()
			if !ok || p.Seq != want {
				t.Fatalf("pop = %+v, %v; want seq %d", p, ok, want)
			}
			want++
		}
	}
	for q.len() > 0 {
		p, _ := q.pop()
		if p.Seq != want {
			t.Fatalf("pop = %d, want %d", p.Seq, want)
		}
		want++
	}
	if want != next {
		t.Fatalf("popped %d packets, pushed %d", want, next)
	}
	if q.rel.moved > int(next) {
		t.Fatalf("compaction moved %d packets for %d pushed, want at most one move per packet", q.rel.moved, next)
	}
}

func TestPacketQueueReleasesPoppedPayloads(t *testing.T) {
	var q packetQueue
	q.push(Packet{Payload: []byte("a")})
	q.push(Packet{Payload: []byte("b")})
	if _, ok := q.pop(); !ok {
		t.Fatal("pop failed")
	}
	if q.rel.buf[0].pkt.Payload != nil {
		t.Fatal("a popped packet's payload is still referenced by the queue")
	}
}

func BenchmarkLoopbackRecvDrain(b *testing.B) {
	hub := NewLoopbackHub(LoopbackConfig{})
	a := hub.AddPeer(1)
	c := hub.AddPeer(2)
	payload := []byte("x")
	for b.Loop() {
		for range 10000 {
			_ = a.Send(2, ChannelUnreliable, payload)
		}
		for {
			if _, ok := c.Recv(); !ok {
				break
			}
		}
	}
}

// A burst grows the buffer; once it is drained the queue gives the buffer back instead of holding the burst's capacity for good.
func TestPacketQueueReleasesItsBufferAfterABurst(t *testing.T) {
	var q packetQueue
	for i := range 10 * packetFIFOKeep {
		q.push(Packet{Seq: uint32(i)})
	}
	for q.len() > 0 {
		q.pop()
	}
	if c := cap(q.rel.buf); c > packetFIFOKeep {
		t.Fatalf("drained queue keeps capacity %d, want at most %d", c, packetFIFOKeep)
	}
	q.push(Packet{Seq: 1})
	if p, ok := q.pop(); !ok || p.Seq != 1 {
		t.Fatalf("pop after the burst = %+v, %v", p, ok)
	}
}
