package datatable

import (
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/nsgo-dev/game/pkg/tooling/hotreload"
)

// Table holds a typed game data table loaded from disk and supports safe
// hot reloading via tooling/hotreload.
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
	t.mu.Unlock()
	t.version.Add(1)
	return nil
}

// Watch registers the table's file with watcher and invokes onReload with the updated
// rows on the engine goroutine whenever a valid edit is saved.
//
// Invalid edits keep the previous valid rows intact and record the error in LastError.
func (t *Table[T]) Watch(watcher *hotreload.Watcher, onReload func([]T)) {
	t.WatchWithError(watcher, func(rows []T, err error) {
		if err == nil && onReload != nil {
			onReload(rows)
		}
	})
}

// WatchWithError registers the table's file with watcher and invokes onReload with
// either the new rows (err == nil) or the parse error (rows == previous valid rows).
func (t *Table[T]) WatchWithError(watcher *hotreload.Watcher, onReload func([]T, error)) {
	if t == nil || watcher == nil {
		return
	}
	relPath := t.path
	if dir := watcher.Dir(); dir != "" {
		absDir, err1 := filepath.Abs(dir)
		absFile, err2 := filepath.Abs(t.path)
		if err1 == nil && err2 == nil {
			if rel, err := filepath.Rel(absDir, absFile); err == nil {
				relPath = rel
			}
		}
	}
	watcher.Watch(relPath, func(data []byte) {
		err := t.ReloadBytes(data)
		if onReload != nil {
			onReload(t.Rows(), err)
		}
	})
}
