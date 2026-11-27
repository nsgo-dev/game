//go:build !release

package devstate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
)

var (
	timeType     = reflect.TypeOf(time.Time{})
	durationType = reflect.TypeOf(time.Duration(0))
)

// ComputeSchemaHash reflects the deterministic structural schema of sample
// (field names, tags, and types recursively) and returns its SHA-256 hex digest.
//
// Top-level pointer indirection is unwrapped so ComputeSchemaHash(T{}) and
// ComputeSchemaHash(&T{}) yield the exact same hash. Any added, removed,
// renamed, reordered, or retyped field changes the digest.
func ComputeSchemaHash(sample any) string {
	var b strings.Builder
	if sample == nil {
		b.WriteString("nil")
	} else {
		t := reflect.TypeOf(sample)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		seen := make(map[reflect.Type]int)
		describeType(&b, t, seen)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func describeType(b *strings.Builder, t reflect.Type, seen map[reflect.Type]int) {
	if t == nil {
		b.WriteString("nil")
		return
	}
	if t == timeType {
		b.WriteString("time.Time")
		return
	}
	if t == durationType {
		b.WriteString("time.Duration")
		return
	}

	switch t.Kind() {
	case reflect.Pointer:
		b.WriteByte('*')
		describeType(b, t.Elem(), seen)
	case reflect.Slice:
		b.WriteString("[]")
		describeType(b, t.Elem(), seen)
	case reflect.Array:
		b.WriteByte('[')
		b.WriteString(strconv.Itoa(t.Len()))
		b.WriteByte(']')
		describeType(b, t.Elem(), seen)
	case reflect.Map:
		b.WriteString("map[")
		describeType(b, t.Key(), seen)
		b.WriteByte(']')
		describeType(b, t.Elem(), seen)
	case reflect.Struct:
		if id, ok := seen[t]; ok {
			b.WriteString("rec#")
			b.WriteString(strconv.Itoa(id))
			return
		}
		seen[t] = len(seen)
		b.WriteString("struct{")
		for i := range t.NumField() {
			if i > 0 {
				b.WriteByte(';')
			}
			f := t.Field(i)
			if f.Anonymous {
				b.WriteString("emb:")
			}
			if !f.IsExported() {
				b.WriteString("unexp:")
			}
			b.WriteString(f.Name)
			b.WriteByte(':')
			describeType(b, f.Type, seen)
			if tag := f.Tag.Get("json"); tag != "" {
				b.WriteString("[json=")
				b.WriteString(tag)
				b.WriteByte(']')
			}
		}
		b.WriteByte('}')
	default:
		b.WriteString(t.Kind().String())
	}
}

// Store persists and restores SessionSnapshots either on disk or in core.Storage.
type Store struct {
	mu         sync.Mutex
	path       string
	useStorage bool
	storage    core.Storage
	storageKey string
	logf       func(format string, args ...any)
}

// NewStore creates a file-backed dev session store at path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// NewStorageStore creates a core.Storage-backed dev session store under key.
func NewStorageStore(st core.Storage, key string) *Store {
	if key == "" {
		key = "devstate.session"
	}
	return &Store{
		useStorage: true,
		storage:    st,
		storageKey: key,
	}
}

// SetLogger sets the logger used for non-fatal restore notices such as schema mismatches.
// Passing nil restores the default log.Printf behavior.
func (s *Store) SetLogger(logf func(format string, args ...any)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.logf = logf
	s.mu.Unlock()
}

func (s *Store) logNotice(format string, args ...any) {
	s.mu.Lock()
	fn := s.logf
	s.mu.Unlock()
	if fn != nil {
		fn(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Save marshals state together with its schema hash and loop clock, writing the
// snapshot atomically.
func (s *Store) Save(sceneID string, loopTick uint64, elapsed time.Duration, state any) error {
	if s == nil {
		return errors.New("devstate: nil store")
	}
	if state == nil {
		return errors.New("devstate: state must not be nil")
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("devstate: marshal state: %w", err)
	}
	snap := SessionSnapshot{
		SceneID:     sceneID,
		SchemaHash:  ComputeSchemaHash(state),
		LoopTick:    loopTick,
		LoopElapsed: elapsed,
		Payload:     payload,
		SavedAt:     time.Now().UTC(),
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("devstate: marshal snapshot: %w", err)
	}
	return s.writeRaw(raw)
}

// LoadSnapshot reads the stored SessionSnapshot without decoding its payload.
// If no snapshot exists, ok is false and err is nil. A snapshot that does not decode returns a *loaderr.Error that names the snapshot's file (or storage key) and line.
func (s *Store) LoadSnapshot() (snap SessionSnapshot, ok bool, err error) {
	if s == nil {
		return SessionSnapshot{}, false, errors.New("devstate: nil store")
	}
	raw, exists, err := s.readRaw()
	if err != nil || !exists {
		return SessionSnapshot{}, false, err
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return SessionSnapshot{}, true, loaderr.Locate(err, s.name(), raw, "delete the snapshot so the next run starts fresh; Save writes a new one")
	}
	return snap, true, nil
}

// Restore loads the saved session snapshot into state (which must be a non-nil pointer).
//
// If no snapshot exists, or if the saved schema hash does not match state's current
// schema, Restore logs a diagnostic notice and returns Restored: false with a clear
// SkipReason and a nil error so the app starts fresh without crashing.
func (s *Store) Restore(state any) (RestoreResult, error) {
	if s == nil {
		return RestoreResult{}, errors.New("devstate: nil store")
	}
	if state == nil {
		return RestoreResult{}, errors.New("devstate: restore target must not be nil")
	}
	rv := reflect.ValueOf(state)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return RestoreResult{}, errors.New("devstate: restore target must be a non-nil pointer")
	}

	snap, exists, err := s.LoadSnapshot()
	if err != nil {
		if exists {
			reason := fmt.Sprintf("corrupt session snapshot: %v; starting fresh", err)
			s.logNotice("devstate: %s", reason)
			return RestoreResult{Restored: false, SkipReason: reason}, nil
		}
		return RestoreResult{}, err
	}
	if !exists {
		return RestoreResult{Restored: false, SkipReason: "no saved session snapshot"}, nil
	}

	wantHash := ComputeSchemaHash(state)
	if snap.SchemaHash != wantHash {
		reason := fmt.Sprintf(
			"schema hash mismatch for scene %q (saved %s, current %s): incompatible state struct change, starting fresh",
			snap.SceneID, shortHash(snap.SchemaHash), shortHash(wantHash),
		)
		s.logNotice("devstate: %s", reason)
		return RestoreResult{
			Restored:    false,
			SceneID:     snap.SceneID,
			LoopTick:    snap.LoopTick,
			LoopElapsed: snap.LoopElapsed,
			SkipReason:  reason,
		}, nil
	}

	if err := json.Unmarshal(snap.Payload, state); err != nil {
		reason := fmt.Sprintf("failed to decode snapshot payload for scene %q: %v; starting fresh", snap.SceneID, err)
		s.logNotice("devstate: %s", reason)
		return RestoreResult{
			Restored:    false,
			SceneID:     snap.SceneID,
			LoopTick:    snap.LoopTick,
			LoopElapsed: snap.LoopElapsed,
			SkipReason:  reason,
		}, nil
	}

	return RestoreResult{
		Restored:    true,
		SceneID:     snap.SceneID,
		LoopTick:    snap.LoopTick,
		LoopElapsed: snap.LoopElapsed,
	}, nil
}

// Clear removes any persisted session snapshot.
func (s *Store) Clear() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.useStorage {
		s.storage.RemoveItem(s.storageKey)
		return nil
	}
	if s.path == "" {
		return nil
	}
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("devstate: clear snapshot: %w", err)
	}
	return nil
}

func (s *Store) writeRaw(raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.useStorage {
		if err := s.storage.SetItem(s.storageKey, string(raw)); err != nil {
			return fmt.Errorf("devstate: storage set %q: %w", s.storageKey, err)
		}
		return nil
	}
	if s.path == "" {
		return errors.New("devstate: empty store path")
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("devstate: mkdir %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".devstate-*.tmp")
	if err != nil {
		if werr := os.WriteFile(s.path, raw, 0o644); werr != nil {
			return fmt.Errorf("devstate: write snapshot: %w", werr)
		}
		return nil
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("devstate: write temp snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("devstate: close temp snapshot: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		_ = os.Remove(tmpName)
		if werr := os.WriteFile(s.path, raw, 0o644); werr != nil {
			return fmt.Errorf("devstate: write snapshot: %w", werr)
		}
	}
	return nil
}

func (s *Store) readRaw() ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.useStorage {
		val, ok := s.storage.GetItem(s.storageKey)
		if !ok {
			return nil, false, nil
		}
		return []byte(val), true, nil
	}
	if s.path == "" {
		return nil, false, errors.New("devstate: empty store path")
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("devstate: read snapshot: %w", err)
	}
	return raw, true, nil
}

// name names the snapshot in errors: its file, or its storage key on the web.
func (s *Store) name() string {
	if s.useStorage {
		return s.storageKey
	}
	return s.path
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	if h == "" {
		return "<empty>"
	}
	return h
}

// Hook binds a Snapshotter to a core.Scope so state is saved automatically on
// exit, scope disposal, or an optional periodic interval.
type Hook struct {
	cx     *core.Scope
	store  *Store
	target Snapshotter
	opts   HookOptions

	mu        sync.Mutex
	stopTimer func() bool
	stopped   atomic.Bool
}

// Attach wires target into cx so that session snapshots are saved automatically
// when cx exits or is disposed, and periodically when opts.SaveInterval > 0.
func Attach(cx *core.Scope, store *Store, target Snapshotter, opts HookOptions) *Hook {
	h := &Hook{
		cx:     cx,
		store:  store,
		target: target,
		opts:   opts,
	}
	if store != nil && opts.Logf != nil {
		store.SetLogger(opts.Logf)
	}
	if cx == nil || store == nil || target == nil {
		return h
	}

	cx.OnExit(func(done func()) {
		if !h.stopped.Load() {
			_ = h.SaveNow()
		}
		done()
	})

	cx.Cleanup(func() {
		h.cancelTimer()
		if !h.stopped.Swap(true) {
			_ = h.saveUnlocked()
		}
	})

	if opts.SaveInterval > 0 {
		h.scheduleNext()
	}
	return h
}

func (h *Hook) scheduleNext() {
	if h.stopped.Load() || h.cx == nil || h.cx.Disposed() || h.opts.SaveInterval <= 0 {
		return
	}
	stop := h.cx.AfterFunc(h.opts.SaveInterval, func() {
		if h.stopped.Load() || h.cx.Disposed() {
			return
		}
		_ = h.SaveNow()
		h.scheduleNext()
	})
	h.mu.Lock()
	h.stopTimer = stop
	h.mu.Unlock()
}

func (h *Hook) cancelTimer() {
	h.mu.Lock()
	stop := h.stopTimer
	h.stopTimer = nil
	h.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// SaveNow captures a snapshot from the attached Snapshotter and persists it immediately.
func (h *Hook) SaveNow() error {
	if h == nil || h.store == nil || h.target == nil {
		return nil
	}
	return h.saveUnlocked()
}

func (h *Hook) saveUnlocked() error {
	state, err := h.target.Snapshot()
	if err != nil {
		return err
	}
	sceneID := "default"
	if h.opts.SceneID != nil {
		sceneID = h.opts.SceneID()
	}
	var tick uint64
	if h.opts.LoopTick != nil {
		tick = h.opts.LoopTick()
	}
	var elapsed time.Duration
	if h.opts.LoopElapsed != nil {
		elapsed = h.opts.LoopElapsed()
	}
	return h.store.Save(sceneID, tick, elapsed, state)
}

// RestoreInto loads a saved snapshot into state (a non-nil pointer) and, when
// successfully restored, forwards it to the attached Snapshotter's Restore method.
func (h *Hook) RestoreInto(state any) (RestoreResult, error) {
	if h == nil || h.store == nil {
		return RestoreResult{Restored: false, SkipReason: "no devstate store attached"}, nil
	}
	res, err := h.store.Restore(state)
	if err != nil || !res.Restored {
		return res, err
	}
	if h.target != nil {
		if err := h.target.Restore(state); err != nil {
			return RestoreResult{
				Restored:    false,
				SceneID:     res.SceneID,
				LoopTick:    res.LoopTick,
				LoopElapsed: res.LoopElapsed,
				SkipReason:  fmt.Sprintf("snapshotter restore failed: %v", err),
			}, err
		}
	}
	return res, nil
}

// Stop halts periodic checkpointing and prevents automatic save on scope cleanup.
func (h *Hook) Stop() {
	if h == nil {
		return
	}
	h.stopped.Store(true)
	h.cancelTimer()
}
