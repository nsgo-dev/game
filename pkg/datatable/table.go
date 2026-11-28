package datatable

import (
	"sync"
	"sync/atomic"
)

// Table holds a typed game data table loaded from disk. datatablewatch adds
// hot reloading via tooling/hotreload for game clients; headless builds such
// as a dedicated server load tables without it.
//
// If a reload fails (for example due to a typo in a cell during editing), Table
// preserves the last valid rows and records the error in LastError.
type Table[T any] struct {
	path string

	mu      sync.RWMutex
	rows    []T
	lastErr error
	version atomic.Uint64
}

// View is an immutable, zero-copy read view of a Table at one version.
// Callers must not modify Rows. A later reload publishes a new view and does
// not change an existing View.
type View[T any] struct {
	rows    []T
	version uint64
}

// Rows returns the shared rows in this view. The slice must be treated as read-only.
func (v View[T]) Rows() []T { return v.rows }

// Version returns the table generation captured by this view.
func (v View[T]) Version() uint64 { return v.version }

// NewTable loads the typed table at path immediately and initializes Version to 1.
func NewTable[T any](path string) (*Table[T], error) {
	rows, err := LoadFile[T](path)
	if err != nil {
		return nil, err
	}
	t := &Table[T]{
		path: path,
		rows: rows,
	}
	t.version.Store(1)
	return t, nil
}

// Path returns the table's file path.
func (t *Table[T]) Path() string {
	if t == nil {
		return ""
	}
	return t.path
}

// Rows returns a copy of the currently loaded rows.
func (t *Table[T]) Rows() []T {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	out := make([]T, len(t.rows))
	copy(out, t.rows)
	t.mu.RUnlock()
	return out
}

// View returns an immutable snapshot of the current rows and version without
// copying the row slice. The snapshot remains stable across Reload calls.
func (t *Table[T]) View() View[T] {
	if t == nil {
		return View[T]{}
	}
	t.mu.RLock()
	rows := t.rows
	version := t.version.Load()
	t.mu.RUnlock()
	return View[T]{rows: rows, version: version}
}

// Version returns the monotonically increasing load generation (starting at 1).
func (t *Table[T]) Version() uint64 {
	if t == nil {
		return 0
	}
	return t.version.Load()
}

// LastError returns the error from the most recent reload attempt, or nil if it succeeded.
func (t *Table[T]) LastError() error {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	err := t.lastErr
	t.mu.RUnlock()
	return err
}

// Reload re-reads the file from disk. On success, it atomically replaces Rows and
// increments Version. On failure, existing Rows are preserved and LastError is updated.
func (t *Table[T]) Reload() error {
	rows, err := LoadFile[T](t.path)
	return t.applyReload(rows, err)
}

// ReloadBytes parses raw file bytes using the table's file extension. On success,
// it atomically replaces Rows and increments Version.
func (t *Table[T]) ReloadBytes(data []byte) error {
	rows, err := loadByExtension[T](t.path, data)
	return t.applyReload(rows, err)
}

func (t *Table[T]) applyReload(rows []T, err error) error {
	t.mu.Lock()
	if err != nil {
		t.lastErr = err
		t.mu.Unlock()
		return err
	}
	t.rows = rows
	t.lastErr = nil
	t.version.Add(1)
	t.mu.Unlock()
	return nil
}
