//go:build !js

package netplay

import (
	"sync"
	"sync/atomic"
	"time"
)

// outboxFrame is one encoded frame waiting for a client's writer.
type outboxFrame struct {
	reliable bool
	data     []byte
}

// outboxItem is what the writer takes from an outbox: a queued frame, or, once finish was called and the queue has drained, the final frame. flushBy is finish's deadline, zero until finish is called.
type outboxItem struct {
	data    []byte
	final   bool
	flushBy time.Time
}

// outboxCounters are a server's outbox counters, shared by the outboxes of all its clients so that they outlive any one connection. The server counts overflows itself, once per session it ends.
type outboxCounters struct {
	dropped       atomic.Uint64
	overflows     atomic.Uint64
	datagramsSent atomic.Uint64
}

// pushResult is what outbox.push did with a frame.
type pushResult int

const (
	pushQueued   pushResult = iota // the frame is queued for the writer
	pushDropped                    // the frame was unreliable and did not fit, so it was dropped
	pushOverflow                   // the frame was reliable and did not fit: the caller ends the client's session
	pushClosed                     // the outbox is closed because the connection ended
)

// outbox is one client's queue of frames that Send has encoded and its writer goroutine has not written yet (see OutboxLimits). Send pushes without blocking, the writer takes frames with next, finish has the writer drain the queue and then write a final frame, and close ends both at once.
type outbox struct {
	mu         sync.Mutex
	q          fifo[outboxFrame]
	unreliable int // unreliable frames in q
	bytes      int // bytes of the frames in q
	seq        [2]uint32
	closed     bool
	finishing  bool          // finish was called: push refuses frames, and next returns final once q is empty
	final      []byte        // the frame next returns after the queue, set by finish
	flushBy    time.Time     // finish's deadline for the queue and final
	wake       chan struct{} // signalled when a frame is queued or the outbox closes; the writer is its only receiver
	counters   *outboxCounters
}

func newOutbox(counters *outboxCounters) *outbox {
	return &outbox{wake: make(chan struct{}, 1), counters: counters}
}

// push numbers a frame on channel ch, encodes it with encode, and queues it within limits, dropping the oldest queued unreliable frames to make room.
func (o *outbox) push(ch Channel, payload []byte, limits OutboxLimits, encode func(ch Channel, seq uint32, payload []byte) []byte) pushResult {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.finishing {
		return pushClosed
	}
	o.seq[ch]++
	f := outboxFrame{reliable: ch == ChannelReliableOrdered, data: encode(ch, o.seq[ch], payload)}
	for !o.fits(len(f.data), limits) {
		if o.dropOldestUnreliable() {
			continue
		}
		if f.reliable {
			return pushOverflow
		}
		o.counters.dropped.Add(1)
		return pushDropped
	}
	o.q.push(f)
	o.bytes += len(f.data)
	if !f.reliable {
		o.unreliable++
	}
	o.signal()
	return pushQueued
}

// fits reports whether a frame of n bytes fits next to the queued frames. Any frame fits an empty outbox.
func (o *outbox) fits(n int, limits OutboxLimits) bool {
	queued := o.q.len()
	if queued == 0 {
		return true
	}
	if c := limits.frameCap(); c > 0 && queued+1 > c {
		return false
	}
	if c := limits.byteCap(); c > 0 && o.bytes+n > c {
		return false
	}
	return true
}

// dropOldestUnreliable drops the oldest queued unreliable frame, reporting false when there is none.
func (o *outbox) dropOldestUnreliable() bool {
	if o.unreliable == 0 {
		return false
	}
	live := o.q.buf[o.q.head:]
	for i, f := range live {
		if f.reliable {
			continue
		}
		copy(live[i:], live[i+1:])
		live[len(live)-1] = outboxFrame{}
		o.q.buf = o.q.buf[:len(o.q.buf)-1]
		o.unreliable--
		o.bytes -= len(f.data)
		o.counters.dropped.Add(1)
		return true
	}
	return false
}

// next waits for the oldest queued frame and removes it, reporting false once the outbox is closed. After finish, next returns the queued frames, then the final frame, which closes the outbox.
func (o *outbox) next() (outboxItem, bool) {
	for {
		o.mu.Lock()
		if o.closed {
			o.mu.Unlock()
			return outboxItem{}, false
		}
		if f, ok := o.q.pop(); ok {
			o.bytes -= len(f.data)
			if !f.reliable {
				o.unreliable--
			}
			item := outboxItem{data: f.data, flushBy: o.flushBy}
			o.mu.Unlock()
			return item, true
		}
		if o.finishing {
			o.closed = true
			item := outboxItem{data: o.final, final: true, flushBy: o.flushBy}
			o.mu.Unlock()
			return item, true
		}
		o.mu.Unlock()
		<-o.wake
	}
}

// finish refuses further frames and has the writer write the frames already queued, then final, all by flushBy. It reports false when the outbox is already closed, so no writer will write final. A second finish keeps the first one's frame and deadline.
func (o *outbox) finish(final []byte, flushBy time.Time) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return false
	}
	if !o.finishing {
		o.finishing = true
		o.final = final
		o.flushBy = flushBy
		o.signal()
	}
	return true
}

// close discards the queued frames and stops the writer; later pushes report pushClosed.
func (o *outbox) close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	o.closed = true
	o.q = fifo[outboxFrame]{}
	o.unreliable, o.bytes = 0, 0
	o.signal()
}

// stats returns the frames and bytes queued.
func (o *outbox) stats() (frames, bytes int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.q.len(), o.bytes
}

func (o *outbox) signal() {
	select {
	case o.wake <- struct{}{}:
	default:
	}
}
