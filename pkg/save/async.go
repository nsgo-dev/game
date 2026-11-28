package save

import (
	"context"
	"errors"
	"reflect"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
)

// ErrAsyncUnavailable reports that an asynchronous save operation needs a
// filesystem-backed Store. core.Storage must be accessed on its engine
// goroutine, so use Save and Load for storage-backed stores.
var ErrAsyncUnavailable = errors.New("save: async operations unavailable for this backend")

type saveAsyncOp struct {
	cx   *core.Scope
	ctx  context.Context
	run  func() error
	done func(error)
}

type saveQueue struct {
	mu      sync.Mutex
	queue   []saveAsyncOp
	running bool
	idle    chan struct{}
}

func (q *saveQueue) enqueue(op saveAsyncOp) {
	q.mu.Lock()
	q.queue = append(q.queue, op)
	if !q.running {
		q.running = true
		q.idle = make(chan struct{})
		go q.run()
	}
	q.mu.Unlock()
}

func (q *saveQueue) run() {
	for {
		q.mu.Lock()
		if len(q.queue) == 0 {
			q.running = false
			close(q.idle)
			q.mu.Unlock()
			return
		}
		op := q.queue[0]
		q.queue = q.queue[1:]
		q.mu.Unlock()

		err := op.ctx.Err()
		if err == nil {
			err = op.run()
		}
		if op.done == nil || op.cx == nil || op.cx.Disposed() {
			continue
		}
		op.cx.Post(func() { op.done(err) })
	}
}

func (q *saveQueue) wait(ctx context.Context) error {
	q.mu.Lock()
	if !q.running {
		q.mu.Unlock()
		return nil
	}
	idle := q.idle
	q.mu.Unlock()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Store) asyncError(cx *core.Scope, done func(error), err error) {
	if done == nil {
		return
	}
	if cx == nil || cx.Disposed() {
		done(err)
		return
	}
	cx.Post(func() { done(err) })
}

// SaveAsync captures the encoded value on the calling goroutine, then writes
// it in order on a worker. done runs on cx's engine goroutine. Calls made on
// the same Store
// are serialized, so concurrent autosaves cannot overwrite one another out of
// order. A cancelled context skips a queued operation.
func (s *Store) SaveAsync(cx *core.Scope, ctx context.Context, slot string, value any, done func(error)) {
	if cx == nil {
		s.asyncError(nil, done, errors.New("save: SaveAsync needs a non-nil scope"))
		return
	}
	if !s.backgroundIO {
		s.asyncError(cx, done, ErrAsyncUnavailable)
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload, err := s.encode(slot, nil, value)
	if err != nil {
		s.asyncError(cx, done, err)
		return
	}
	s.async.enqueue(saveAsyncOp{
		cx: cx, ctx: ctx,
		run:  func() error { return s.writePayload(slot, payload) },
		done: done,
	})
}

// LoadAsync reads and decodes a slot on a worker, then assigns the decoded
// value to dst on cx's engine goroutine. dst must be a non-nil pointer. done
// runs on cx's engine goroutine after the assignment, or with an error when
// the load fails.
func (s *Store) LoadAsync(cx *core.Scope, ctx context.Context, slot string, dst any, done func(error)) {
	if cx == nil {
		s.asyncError(nil, done, errors.New("save: LoadAsync needs a non-nil scope"))
		return
	}
	if !s.backgroundIO {
		s.asyncError(cx, done, ErrAsyncUnavailable)
		return
	}
	rv := reflect.ValueOf(dst)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		s.asyncError(cx, done, errors.New("save: LoadAsync needs a non-nil pointer destination"))
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	temp := reflect.New(rv.Elem().Type())
	target := s.version()
	migrations := cloneMigrations(s.Migrations)
	s.async.enqueue(saveAsyncOp{
		cx: cx, ctx: ctx,
		run: func() error {
			s.ioMu.Lock()
			defer s.ioMu.Unlock()
			return s.loadChecked(slot, temp.Interface(), nil, target, migrations)
		},
		done: func(err error) {
			if err == nil {
				rv.Elem().Set(temp.Elem())
			}
			if done != nil {
				done(err)
			}
		},
	})
}

func cloneMigrations(in map[int]Migration) map[int]Migration {
	if len(in) == 0 {
		return nil
	}
	out := make(map[int]Migration, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Flush waits until all queued asynchronous operations have finished writing
// or loading. It does not wait for callbacks already posted to cx. A nil
// context means context.Background().
func (s *Store) Flush(ctx context.Context) error {
	if !s.backgroundIO {
		return ErrAsyncUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.async.wait(ctx)
}
