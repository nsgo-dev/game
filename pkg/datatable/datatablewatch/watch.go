// Package datatablewatch adds hot reload to datatable.Table through a
// tooling/hotreload.Watcher.
//
// It lives outside datatable so a headless build, such as a dedicated server,
// imports datatable without NSGo: only the game client links the watcher.
package datatablewatch

import (
	"path/filepath"

	"github.com/nsgo-dev/game/pkg/datatable"
	"github.com/nsgo-dev/game/pkg/tooling/hotreload"
)

// Watch registers tbl's file with watcher and invokes onReload with the updated
// rows on the engine goroutine whenever a valid edit is saved.
//
// Invalid edits keep the previous valid rows intact and record the error in
// tbl.LastError.
func Watch[T any](tbl *datatable.Table[T], watcher *hotreload.Watcher, onReload func([]T)) {
	WatchWithError(tbl, watcher, func(rows []T, err error) {
		if err == nil && onReload != nil {
			onReload(rows)
		}
	})
}

// WatchWithError registers tbl's file with watcher and invokes onReload with
// either the new rows (err == nil) or the parse error (rows == previous valid rows).
func WatchWithError[T any](tbl *datatable.Table[T], watcher *hotreload.Watcher, onReload func([]T, error)) {
	if tbl == nil || watcher == nil {
		return
	}
	relPath := tbl.Path()
	if dir := watcher.Dir(); dir != "" {
		absDir, err1 := filepath.Abs(dir)
		absFile, err2 := filepath.Abs(tbl.Path())
		if err1 == nil && err2 == nil {
			if rel, err := filepath.Rel(absDir, absFile); err == nil {
				relPath = rel
			}
		}
	}
	watcher.Watch(relPath, func(data []byte) {
		err := tbl.ReloadBytes(data)
		if onReload != nil {
			onReload(tbl.Rows(), err)
		}
	})
}
