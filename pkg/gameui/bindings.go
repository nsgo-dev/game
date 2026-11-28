package gameui

import (
	"errors"
	"fmt"
	"sync"
)

// Bindings resolves named screen data paths without polling. Sources are read
// when a screen requests a value, and can be replaced atomically.
type Bindings struct {
	mu     sync.RWMutex
	values map[string]any
}

func NewBindings() *Bindings { return &Bindings{values: map[string]any{}} }
func (b *Bindings) Set(path string, value any) error {
	if path == "" {
		return errors.New("gameui: empty binding path")
	}
	b.mu.Lock()
	b.values[path] = value
	b.mu.Unlock()
	return nil
}
func (b *Bindings) Func(path string, fn func() any) error {
	if fn == nil {
		return errors.New("gameui: nil binding function")
	}
	return b.Set(path, fn)
}
func (b *Bindings) Get(path string) (any, bool) {
	b.mu.RLock()
	v, ok := b.values[path]
	b.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if fn, ok := v.(func() any); ok {
		return fn(), true
	}
	return v, true
}
func (b *Bindings) String(path string) (string, error) {
	v, ok := b.Get(path)
	if !ok {
		return "", fmt.Errorf("gameui: binding %q is not registered", path)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("gameui: binding %q is %T, want string", path, v)
	}
	return s, nil
}
func (b *Bindings) Int(path string) (int, error) {
	v, ok := b.Get(path)
	if !ok {
		return 0, fmt.Errorf("gameui: binding %q is not registered", path)
	}
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	}
	return 0, fmt.Errorf("gameui: binding %q is %T, want int", path, v)
}
func (b *Bindings) Bool(path string) (bool, error) {
	v, ok := b.Get(path)
	if !ok {
		return false, fmt.Errorf("gameui: binding %q is not registered", path)
	}
	x, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("gameui: binding %q is %T, want bool", path, v)
	}
	return x, nil
}
