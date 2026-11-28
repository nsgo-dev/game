package assets

import "sync"

// RefCache shares a decoded value by URI and releases it after the last owner
// calls Release. It is a small primitive for manager integrations.
type RefCache[T any] struct {
	mu     sync.Mutex
	values map[string]*refEntry[T]
}
type refEntry[T any] struct {
	value T
	refs  int
	close func(T)
}
type Ref[T any] struct {
	c     *RefCache[T]
	key   string
	value T
	once  sync.Once
}

func NewRefCache[T any]() *RefCache[T] { return &RefCache[T]{values: map[string]*refEntry[T]{}} }
func (c *RefCache[T]) Acquire(key string, load func() (T, error), close func(T)) (Ref[T], error) {
	c.mu.Lock()
	if e := c.values[key]; e != nil {
		e.refs++
		v := e.value
		c.mu.Unlock()
		return Ref[T]{c: c, key: key, value: v}, nil
	}
	c.mu.Unlock()
	v, err := load()
	if err != nil {
		return Ref[T]{}, err
	}
	c.mu.Lock()
	if e := c.values[key]; e != nil {
		e.refs++
		c.mu.Unlock()
		if close != nil {
			close(v)
		}
		return Ref[T]{c: c, key: key, value: e.value}, nil
	}
	c.values[key] = &refEntry[T]{value: v, refs: 1, close: close}
	c.mu.Unlock()
	return Ref[T]{c: c, key: key, value: v}, nil
}
func (r Ref[T]) Value() T { return r.value }
func (r *Ref[T]) Release() {
	if r == nil || r.c == nil {
		return
	}
	r.once.Do(func() {
		r.c.mu.Lock()
		e := r.c.values[r.key]
		if e != nil {
			e.refs--
			if e.refs == 0 {
				delete(r.c.values, r.key)
				if e.close != nil {
					e.close(e.value)
				}
			}
		}
		r.c.mu.Unlock()
	})
}
