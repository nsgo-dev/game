package editor

import (
	"context"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
)

// editorHeadless gives a scope-less Editor a single serialized owner so background callbacks
// (Post, agent/automation/first-run/task/asset/export/start-screen callbacks, timers, and
// editormcp.Server with nil Options.Run) serialize with each other and with callers driving
// the editor via RunHeadless, and AttachScope drains any in-flight headless work before the
// UI goroutine takes ownership.
type editorHeadless struct {
	mu       sync.Mutex
	scope    *core.Scope
	active   bool          // true while RunHeadless, PostOrRunHeadless, detachScope, or drain owns the Editor
	draining bool          // true while a background drain goroutine is scheduled or running
	queue    []func()      // FIFO queue of async callbacks posted via Post while no scope is attached
	idleCh   chan struct{} // closed whenever active transitions from true to false
}

func (h *editorHeadless) getScope() *core.Scope {
	h.mu.Lock()
	cx := h.scope
	h.mu.Unlock()
	return cx
}

func (h *editorHeadless) hasScope() bool {
	cx := h.getScope()
	return cx != nil && !cx.Disposed()
}

func (h *editorHeadless) post(fn func()) {
	if fn == nil {
		return
	}
	h.mu.Lock()
	if cx := h.scope; cx != nil && !cx.Disposed() {
		h.mu.Unlock()
		cx.Post(fn)
		return
	}
	h.queue = append(h.queue, fn)
	if !h.draining && !h.active {
		h.draining = true
		go h.drain()
	}
	h.mu.Unlock()
}

func (h *editorHeadless) drain() {
	defer func() {
		h.mu.Lock()
		if h.draining {
			if len(h.queue) > 0 {
				if cx := h.scope; cx != nil && !cx.Disposed() {
					pending := h.queue
					h.queue = nil
					h.draining = false
					h.mu.Unlock()
					for _, fn := range pending {
						cx.Post(fn)
					}
					return
				}
				go h.drain()
			} else {
				h.draining = false
			}
		}
		h.mu.Unlock()
	}()

	for {
		h.mu.Lock()
		for h.active {
			ch := h.idleCh
			h.mu.Unlock()
			<-ch
			h.mu.Lock()
		}
		if len(h.queue) == 0 {
			h.draining = false
			h.mu.Unlock()
			return
		}
		if cx := h.scope; cx != nil && !cx.Disposed() {
			pending := h.queue
			h.queue = nil
			h.draining = false
			h.mu.Unlock()
			for _, fn := range pending {
				cx.Post(fn)
			}
			return
		}
		fn := h.queue[0]
		h.queue[0] = nil
		h.queue = h.queue[1:]
		if len(h.queue) == 0 {
			h.queue = nil
		}
		h.active = true
		h.idleCh = make(chan struct{})
		h.mu.Unlock()

		func() {
			defer h.release()
			fn()
		}()
	}
}

func (h *editorHeadless) release() {
	h.mu.Lock()
	h.active = false
	ch := h.idleCh
	h.idleCh = nil
	if ch != nil {
		close(ch)
	}
	var pending []func()
	var postScope *core.Scope
	if len(h.queue) > 0 && !h.draining {
		if cx := h.scope; cx != nil && !cx.Disposed() {
			pending = h.queue
			h.queue = nil
			postScope = cx
		} else {
			h.draining = true
			go h.drain()
		}
	}
	h.mu.Unlock()
	for _, fn := range pending {
		postScope.Post(fn)
	}
}

func (h *editorHeadless) runHeadlessContext(ctx context.Context, fn func()) error {
	if fn == nil {
		return nil
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	h.mu.Lock()
	for h.active {
		ch := h.idleCh
		h.mu.Unlock()
		if ctx == nil {
			<-ch
		} else {
			select {
			case <-ch:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		h.mu.Lock()
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			h.mu.Unlock()
			return err
		}
	}
	h.active = true
	h.idleCh = make(chan struct{})
	h.mu.Unlock()

	defer h.release()
	fn()
	return nil
}

func (h *editorHeadless) postOrRunHeadless(ctx context.Context, fn func()) error {
	if fn == nil {
		return nil
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	h.mu.Lock()
	if cx := h.scope; cx != nil && !cx.Disposed() {
		h.mu.Unlock()
		cx.Post(fn)
		return nil
	}
	for h.active {
		ch := h.idleCh
		h.mu.Unlock()
		if ctx == nil {
			<-ch
		} else {
			select {
			case <-ch:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		h.mu.Lock()
		if cx := h.scope; cx != nil && !cx.Disposed() {
			h.mu.Unlock()
			cx.Post(fn)
			return nil
		}
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			h.mu.Unlock()
			return err
		}
	}
	h.active = true
	h.idleCh = make(chan struct{})
	h.mu.Unlock()

	defer h.release()
	fn()
	return nil
}

func (h *editorHeadless) attachScope(cx *core.Scope) {
	h.mu.Lock()
	h.scope = cx
	for h.active {
		ch := h.idleCh
		h.mu.Unlock()
		<-ch
		h.mu.Lock()
	}
	var pending []func()
	if cx != nil && !cx.Disposed() && len(h.queue) > 0 {
		pending = h.queue
		h.queue = nil
	}
	h.mu.Unlock()
	for _, fn := range pending {
		cx.Post(fn)
	}
}

func (h *editorHeadless) detachScope(fn func()) {
	h.mu.Lock()
	h.scope = nil
	for h.active {
		ch := h.idleCh
		h.mu.Unlock()
		<-ch
		h.mu.Lock()
	}
	h.active = true
	h.idleCh = make(chan struct{})
	h.mu.Unlock()

	defer h.release()
	if fn != nil {
		fn()
	}
}

// HasScope reports whether a UI core.Scope is currently attached to the editor.
func (e *Editor) HasScope() bool {
	if e == nil {
		return false
	}
	return e.headless.hasScope()
}

// Post schedules fn onto the editor's UI scope when attached, or queues fn onto the editor's
// single headless executor otherwise. Post never blocks waiting for the editor owner, so it is
// safe to call from background goroutines and from inside RunHeadless.
func (e *Editor) Post(fn func()) {
	e.postOrRun(fn)
}

func (e *Editor) postOrRun(fn func()) {
	if e == nil || fn == nil {
		return
	}
	e.headless.post(fn)
}

// RunHeadless runs fn synchronously on the caller's goroutine as the sole owner of a scope-less
// Editor, serialized with every headless background callback (Post, timers, agent/automation/
// first-run/task callbacks, and editormcp.Server with nil Options.Run) and drained by AttachScope.
//
// Tests and scripts that drive a scope-less Editor while background work or an MCP server is active
// should call the editor through RunHeadless. Do not nest RunHeadless inside RunHeadless; use Post
// to enqueue follow-up work.
func (e *Editor) RunHeadless(fn func()) {
	if e == nil || fn == nil {
		return
	}
	_ = e.headless.runHeadlessContext(nil, fn)
}

// RunHeadlessContext behaves like RunHeadless, returning ctx.Err() without running fn if ctx ends
// before exclusive headless ownership is acquired.
func (e *Editor) RunHeadlessContext(ctx context.Context, fn func()) error {
	if e == nil || fn == nil {
		return nil
	}
	return e.headless.runHeadlessContext(ctx, fn)
}

// PostOrRunHeadless schedules fn asynchronously on the attached core.Scope when one is live;
// otherwise it acquires the editor's single headless owner (returning ctx.Err() if ctx ends
// before ownership is acquired) and runs fn synchronously on the caller's goroutine.
//
// This is used by editormcp.Server when Options.Run is nil so tool calls and session notifications
// serialize with RunHeadless, Post, and AttachScope without a separate server mutex.
func (e *Editor) PostOrRunHeadless(ctx context.Context, fn func()) error {
	if e == nil || fn == nil {
		return nil
	}
	return e.headless.postOrRunHeadless(ctx, fn)
}
