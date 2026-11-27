package flow

import "reflect"

// Bus is a typed, zero-global-state event bus for communication between objects in a scene or world.
//
// The zero value is ready to use. Handlers run in subscription order; unsubscribing or subscribing inside a handler never skips subsequent subscribers for the event in flight.
type Bus struct {
	channels map[reflect.Type]busChannel
	queue    []func()
}

type busChannel interface {
	clear()
}

type sub[T any] struct {
	active bool
	once   bool
	fn     func(T)
}

type channel[T any] struct {
	subs  []*sub[T]
	depth int
	dirty bool
}

func (c *channel[T]) compact() {
	w := 0
	for _, s := range c.subs {
		if s.active {
			c.subs[w] = s
			w++
		}
	}
	clear(c.subs[w:])
	c.subs = c.subs[:w]
	c.dirty = false
}

func (c *channel[T]) clear() {
	for _, s := range c.subs {
		s.active = false
		s.fn = nil
	}
	c.dirty = true
	if c.depth == 0 {
		clear(c.subs)
		c.subs = c.subs[:0]
		c.dirty = false
	}
}

func channelFor[T any](b *Bus, create bool) *channel[T] {
	typ := reflect.TypeFor[T]()
	if b.channels != nil {
		if ch, ok := b.channels[typ]; ok {
			return ch.(*channel[T])
		}
	}
	if !create {
		return nil
	}
	if b.channels == nil {
		b.channels = make(map[reflect.Type]busChannel)
	}
	ch := &channel[T]{}
	b.channels[typ] = ch
	return ch
}

// Subscribe registers fn to receive events of type T published on b and returns an idempotent unsubscribe function. A nil fn is ignored.
func Subscribe[T any](b *Bus, fn func(T)) (unsubscribe func()) {
	return subscribe(b, fn, false)
}

// SubscribeOnce registers fn to receive the next event of type T published on b, then automatically unsubscribes it before invoking fn. It returns an idempotent unsubscribe function to cancel before it fires.
func SubscribeOnce[T any](b *Bus, fn func(T)) (unsubscribe func()) {
	return subscribe(b, fn, true)
}

func subscribe[T any](b *Bus, fn func(T), once bool) func() {
	if fn == nil {
		return func() {}
	}
	ch := channelFor[T](b, true)
	s := &sub[T]{active: true, once: once, fn: fn}
	ch.subs = append(ch.subs, s)
	return func() {
		if !s.active {
			return
		}
		s.active = false
		s.fn = nil
		ch.dirty = true
		if ch.depth == 0 {
			ch.compact()
		}
	}
}

// Publish synchronously delivers event to every current subscriber of type T on b in subscription order.
//
// Subscribers added during dispatch do not receive the event in flight; subscribers removed during dispatch are skipped if they have not yet run.
func Publish[T any](b *Bus, event T) {
	ch := channelFor[T](b, false)
	if ch == nil || len(ch.subs) == 0 {
		return
	}
	ch.depth++
	defer func() {
		ch.depth--
		if ch.depth == 0 && ch.dirty {
			ch.compact()
		}
	}()

	n := len(ch.subs)
	for i := 0; i < n; i++ {
		s := ch.subs[i]
		if !s.active {
			continue
		}
		fn := s.fn
		if s.once {
			s.active = false
			s.fn = nil
			ch.dirty = true
		}
		fn(event)
	}
}

// Enqueue buffers event on b to be delivered in FIFO order on the next Flush.
func Enqueue[T any](b *Bus, event T) {
	b.queue = append(b.queue, func() { Publish(b, event) })
}

// Flush delivers all events buffered by Enqueue in FIFO order, including any events enqueued by handlers during Flush.
func (b *Bus) Flush() {
	for len(b.queue) > 0 {
		q := b.queue
		b.queue = nil
		for _, deliver := range q {
			deliver()
		}
	}
}

// Clear removes all subscribers and discards all buffered events from b.
func (b *Bus) Clear() {
	for _, ch := range b.channels {
		ch.clear()
	}
	b.channels = nil
	clear(b.queue)
	b.queue = nil
}
