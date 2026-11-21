package save

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

var errEmptyDir = errors.New("save: file backend requires a non-empty directory")

// Backend stores and retrieves raw save payloads and their backup copies by validated slot name.
type Backend interface {
	// Read returns the primary payload for slot, or an error wrapping ErrNotFound if absent.
	Read(slot string) ([]byte, error)
	// ReadBackup returns the previous valid payload kept as a backup for slot, or an error wrapping ErrNotFound if absent.
	ReadBackup(slot string) ([]byte, error)
	// Write saves data as the primary payload for slot, preserving any existing valid primary as the backup first.
	Write(slot string, data []byte) error
	// Delete removes both primary and backup payloads for slot. Deleting an absent slot returns nil.
	Delete(slot string) error
	// Exists reports whether a primary or backup payload exists for slot.
	Exists(slot string) bool
	// Slots returns the sorted names of all slots that have a primary or backup payload.
	Slots() ([]string, error)
}

type storageBackend struct {
	store  core.Storage
	prefix string
}

// StorageBackend returns a Backend that keeps saves in store under keys starting with prefix.
//
// Each slot uses a primary key (<prefix>.slot.<slot>) and a backup key (<prefix>.bak.<slot>), so backup keys never collide with slot names and a failed SetItem (such as *core.StorageFullError) leaves the previous save intact.
func StorageBackend(store core.Storage, prefix string) Backend {
	return &storageBackend{store: store, prefix: prefix}
}

func (b *storageBackend) primaryPrefix() string {
	if b.prefix == "" {
		return "slot."
	}
	return b.prefix + ".slot."
}

func (b *storageBackend) backupPrefix() string {
	if b.prefix == "" {
		return "bak."
	}
	return b.prefix + ".bak."
}

func (b *storageBackend) primaryKey(slot string) string { return b.primaryPrefix() + slot }
func (b *storageBackend) backupKey(slot string) string  { return b.backupPrefix() + slot }

func (b *storageBackend) Read(slot string) ([]byte, error) {
	v, ok := b.store.GetItem(b.primaryKey(slot))
	if !ok {
		return nil, ErrNotFound
	}
	return []byte(v), nil
}

func (b *storageBackend) ReadBackup(slot string) ([]byte, error) {
	v, ok := b.store.GetItem(b.backupKey(slot))
	if !ok {
		return nil, ErrNotFound
	}
	return []byte(v), nil
}

func (b *storageBackend) Write(slot string, data []byte) error {
	primary := b.primaryKey(slot)
	backup := b.backupKey(slot)
	if prev, ok := b.store.GetItem(primary); ok && isValidEnvelope([]byte(prev)) {
		if err := b.store.SetItem(backup, prev); err != nil {
			return fmt.Errorf("save: write backup for %q: %w", slot, err)
		}
	}
	if err := b.store.SetItem(primary, string(data)); err != nil {
		return fmt.Errorf("save: write %q: %w", slot, err)
	}
	return nil
}

func (b *storageBackend) Delete(slot string) error {
	b.store.RemoveItem(b.primaryKey(slot))
	b.store.RemoveItem(b.backupKey(slot))
	return nil
}

func (b *storageBackend) Exists(slot string) bool {
	if _, ok := b.store.GetItem(b.primaryKey(slot)); ok {
		return true
	}
	_, ok := b.store.GetItem(b.backupKey(slot))
	return ok
}

func (b *storageBackend) Slots() ([]string, error) {
	pPref := b.primaryPrefix()
	bPref := b.backupPrefix()
	seen := make(map[string]struct{})
	for _, k := range b.store.Keys() {
		var slot string
		if s, ok := strings.CutPrefix(k, pPref); ok {
			slot = s
		} else if s, ok := strings.CutPrefix(k, bPref); ok {
			slot = s
		} else {
			continue
		}
		if validateSlot(slot) == nil {
			seen[slot] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil, nil
	}
	slots := make([]string, 0, len(seen))
	for slot := range seen {
		slots = append(slots, slot)
	}
	slices.Sort(slots)
	return slots, nil
}

type fileBackend struct {
	dir string
}

// FileBackend returns a Backend that keeps each slot as <slot>.json and its backup as <slot>.json.bak inside dir (such as cx.DataDir()).
//
// Writes stage data into a synced temporary file in dir, preserve any existing valid primary in <slot>.json.bak, and atomically rename the temporary file into place. On the web, where Scope.DataDir is empty, use StorageBackend instead.
func FileBackend(dir string) Backend {
	return &fileBackend{dir: dir}
}

func (b *fileBackend) primaryPath(slot string) string { return filepath.Join(b.dir, slot+".json") }
func (b *fileBackend) backupPath(slot string) string  { return filepath.Join(b.dir, slot+".json.bak") }

func (b *fileBackend) Read(slot string) ([]byte, error) {
	if b.dir == "" {
		return nil, errEmptyDir
	}
	data, err := os.ReadFile(b.primaryPath(slot))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return data, err
}

func (b *fileBackend) ReadBackup(slot string) ([]byte, error) {
	if b.dir == "" {
		return nil, errEmptyDir
	}
	data, err := os.ReadFile(b.backupPath(slot))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return data, err
}

func (b *fileBackend) Write(slot string, data []byte) error {
	if b.dir == "" {
		return errEmptyDir
	}
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return fmt.Errorf("save: create directory %q: %w", b.dir, err)
	}

	tmpName, err := writeTempFile(b.dir, data)
	if err != nil {
		return fmt.Errorf("save: stage %q: %w", slot, err)
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpName)
		}
	}()

	primary := b.primaryPath(slot)
	backup := b.backupPath(slot)
	if prev, err := os.ReadFile(primary); err == nil && isValidEnvelope(prev) {
		if err := writeAtomicFile(b.dir, backup, prev); err != nil {
			return fmt.Errorf("save: write backup for %q: %w", slot, err)
		}
	}

	if err := os.Rename(tmpName, primary); err != nil {
		return fmt.Errorf("save: write %q: %w", slot, err)
	}
	renamed = true
	syncDir(b.dir)
	return nil
}

func (b *fileBackend) Delete(slot string) error {
	if b.dir == "" {
		return errEmptyDir
	}
	var firstErr error
	for _, p := range []string{b.primaryPath(slot), b.backupPath(slot)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return fmt.Errorf("save: delete %q: %w", slot, firstErr)
	}
	syncDir(b.dir)
	return nil
}

func (b *fileBackend) Exists(slot string) bool {
	if b.dir == "" {
		return false
	}
	for _, p := range []string{b.primaryPath(slot), b.backupPath(slot)} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func (b *fileBackend) Slots() ([]string, error) {
	if b.dir == "" {
		return nil, errEmptyDir
	}
	entries, err := os.ReadDir(b.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("save: list slots: %w", err)
	}
	seen := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		var slot string
		if s, ok := strings.CutSuffix(name, ".json.bak"); ok {
			slot = s
		} else if s, ok := strings.CutSuffix(name, ".json"); ok {
			slot = s
		} else {
			continue
		}
		if validateSlot(slot) == nil {
			seen[slot] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil, nil
	}
	slots := make([]string, 0, len(seen))
	for slot := range seen {
		slots = append(slots, slot)
	}
	slices.Sort(slots)
	return slots, nil
}

// writeTempFile writes data to a new temporary file in dir, syncs it to disk and closes it, returning its path.
func writeTempFile(dir string, data []byte) (string, error) {
	tmp, err := os.CreateTemp(dir, ".save-*.tmp")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
			_ = os.Remove(name)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}

func writeAtomicFile(dir, target string, data []byte) error {
	tmpName, err := writeTempFile(dir, data)
	if err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	syncDir(dir)
	return nil
}

// syncDir flushes directory metadata after a rename or removal where the platform supports syncing a directory.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
