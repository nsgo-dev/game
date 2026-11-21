// Package save stores a game's save slots as versioned JSON in either the app's key-value storage (core.Storage) or files in a directory such as cx.DataDir.
//
// Each slot holds a {"version": N, "data": ...} envelope. When a Store loads a save written by an older version, it runs its registered migrations one version at a time (v to v+1) before unmarshaling into the caller's value; a save from a newer version is refused with ErrFutureVersion rather than read into an older shape.
//
// Writes keep the previous valid save as a backup before replacing the primary copy (with a write, sync and rename on disk, or a backup key in core.Storage), so a failed write leaves the last good save intact and a corrupted primary falls back to the backup on Load.
//
// Player settings (audio, bindings, display, locale, accessibility and custom preferences) are persisted separately via SettingsStore and DefaultSettingsStore outside the save-slot namespace, so deleting or clearing save slots never touches player settings, and LoadAndApply wires loaded settings to game subsystems on startup.
package save

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

var (
	// ErrNotFound reports that no save exists in the named slot.
	ErrNotFound = errors.New("save: slot not found")
	// ErrInvalidSlot reports that a slot name is empty or unsafe for storage keys or file paths.
	ErrInvalidSlot = errors.New("save: invalid slot name")
	// ErrFutureVersion reports that a save was written by a newer schema version than the store supports.
	ErrFutureVersion = errors.New("save: version newer than supported")
	// ErrMissingMigration reports that a store lacks a migration step needed to bring a save up to its current version.
	ErrMissingMigration = errors.New("save: missing migration")
	// ErrCorrupt reports that a save's stored payload is not a valid save envelope.
	ErrCorrupt = errors.New("save: corrupt data")
)

// Migration converts a save's raw JSON data from one schema version to the next (v to v+1).
type Migration func(data json.RawMessage) (json.RawMessage, error)

// Option configures a Store returned by NewStore, NewStorageStore or NewFileStore.
type Option func(*Store)

// WithVersion sets the store's current save schema version. Versions below 1 are treated as 1.
func WithVersion(v int) Option {
	return func(s *Store) {
		s.Version = v
	}
}

// WithMigration registers fn to migrate a save's JSON data from version from to from+1.
func WithMigration(from int, fn Migration) Option {
	return func(s *Store) {
		s.RegisterMigration(from, fn)
	}
}

// WithMigrations registers each migration in m, keyed by its starting version (from to from+1).
func WithMigrations(m map[int]Migration) Option {
	return func(s *Store) {
		for from, fn := range m {
			s.RegisterMigration(from, fn)
		}
	}
}

// Store saves and loads versioned JSON game state across named slots backed by a Backend.
type Store struct {
	// Version is the current save schema version written by Save and expected by Load; 1 when 0 or negative.
	Version int
	// Migrations maps a version v to the Migration that upgrades a save's JSON data from v to v+1.
	Migrations map[int]Migration

	backend Backend
}

// NewStore returns a Store backed by backend, defaulting to Version 1 before applying opts.
func NewStore(backend Backend, opts ...Option) *Store {
	s := &Store{
		Version: 1,
		backend: backend,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// NewStorageStore returns a Store backed by store under keys starting with prefix.
func NewStorageStore(store core.Storage, prefix string, opts ...Option) *Store {
	return NewStore(StorageBackend(store, prefix), opts...)
}

// NewFileStore returns a Store backed by JSON files inside dir (such as cx.DataDir()).
func NewFileStore(dir string, opts ...Option) *Store {
	return NewStore(FileBackend(dir), opts...)
}

// RegisterMigration registers fn to upgrade a save's JSON data from version from to from+1.
func (s *Store) RegisterMigration(from int, fn Migration) {
	if s.Migrations == nil {
		s.Migrations = make(map[int]Migration)
	}
	s.Migrations[from] = fn
}

// Save encodes value into a versioned JSON envelope and writes it to slot.
//
// Marshaling happens before the backend is touched, so an encoding error leaves any previous save in slot untouched.
func (s *Store) Save(slot string, value any) error {
	if err := validateSlot(slot); err != nil {
		return err
	}
	if s.backend == nil {
		return errors.New("save: store has no backend")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("save: marshal %q: %w", slot, err)
	}
	payload, err := json.Marshal(envelope{
		Version: s.version(),
		Data:    json.RawMessage(raw),
	})
	if err != nil {
		return fmt.Errorf("save: encode %q: %w", slot, err)
	}
	return s.backend.Write(slot, payload)
}

// Load reads slot, migrates older save versions up to s.Version one step at a time, and unmarshals the data into dst.
//
// If the primary copy is missing, unreadable or corrupted, Load automatically recovers from the slot's backup copy. A save whose version exceeds s.Version returns an error wrapping ErrFutureVersion without falling back to an older backup.
func (s *Store) Load(slot string, dst any) error {
	if err := validateSlot(slot); err != nil {
		return err
	}
	if s.backend == nil {
		return errors.New("save: store has no backend")
	}
	primary, primaryErr := s.backend.Read(slot)
	if primaryErr == nil {
		err := s.decode(slot, primary, dst)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrFutureVersion) || errors.Is(err, ErrMissingMigration) {
			return err
		}
		if backup, bakErr := s.backend.ReadBackup(slot); bakErr == nil {
			recErr := s.decode(slot, backup, dst)
			if recErr == nil {
				return nil
			}
			if errors.Is(recErr, ErrFutureVersion) || errors.Is(recErr, ErrMissingMigration) {
				return recErr
			}
		}
		return err
	}
	backup, bakErr := s.backend.ReadBackup(slot)
	if bakErr != nil {
		if errors.Is(primaryErr, ErrNotFound) && errors.Is(bakErr, ErrNotFound) {
			return fmt.Errorf("save: load %q: %w", slot, ErrNotFound)
		}
		return fmt.Errorf("save: load %q: %w", slot, primaryErr)
	}
	return s.decode(slot, backup, dst)
}

// Delete removes slot and its backup copy. Deleting a slot that does not exist returns nil.
func (s *Store) Delete(slot string) error {
	if err := validateSlot(slot); err != nil {
		return err
	}
	if s.backend == nil {
		return errors.New("save: store has no backend")
	}
	return s.backend.Delete(slot)
}

// DeleteAll removes every save slot and its backup copy. Player settings stored alongside save slots are never touched.
func (s *Store) DeleteAll() error {
	if s.backend == nil {
		return errors.New("save: store has no backend")
	}
	slots, err := s.Slots()
	if err != nil {
		return err
	}
	var errs []error
	for _, slot := range slots {
		if err := s.backend.Delete(slot); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Exists reports whether slot has a stored save or backup copy.
func (s *Store) Exists(slot string) bool {
	if validateSlot(slot) != nil || s.backend == nil {
		return false
	}
	return s.backend.Exists(slot)
}

// Slots returns the sorted names of all slots stored in the backend.
func (s *Store) Slots() ([]string, error) {
	if s.backend == nil {
		return nil, errors.New("save: store has no backend")
	}
	raw, err := s.backend.Slots()
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	slots := make([]string, 0, len(raw))
	for _, slot := range raw {
		if validateSlot(slot) == nil {
			slots = append(slots, slot)
		}
	}
	if len(slots) == 0 {
		return nil, nil
	}
	return slots, nil
}

func (s *Store) version() int {
	if s.Version <= 0 {
		return 1
	}
	return s.Version
}

func (s *Store) decode(slot string, payload []byte, dst any) error {
	return decodeEnvelope(slot, payload, s.version(), s.Migrations, dst)
}

func decodeEnvelope(name string, payload []byte, target int, migrations map[int]Migration, dst any) error {
	env, err := parseEnvelope(payload)
	if err != nil {
		return fmt.Errorf("save: load %q: %w", name, err)
	}
	if env.Version > target {
		return fmt.Errorf("save: load %q: save version %d exceeds current version %d: %w", name, env.Version, target, ErrFutureVersion)
	}
	data := env.Data
	for v := env.Version; v < target; v++ {
		var fn Migration
		if migrations != nil {
			fn = migrations[v]
		}
		if fn == nil {
			return fmt.Errorf("save: load %q: no migration from version %d to %d: %w", name, v, v+1, ErrMissingMigration)
		}
		next, err := fn(data)
		if err != nil {
			return fmt.Errorf("save: load %q: migrate version %d to %d: %w", name, v, v+1, err)
		}
		if len(next) == 0 || !json.Valid(next) {
			return fmt.Errorf("save: load %q: migration %d to %d returned invalid JSON: %w", name, v, v+1, ErrCorrupt)
		}
		data = next
	}
	if err := unmarshalClean(data, dst); err != nil {
		return fmt.Errorf("save: load %q: unmarshal: %w", name, err)
	}
	return nil
}

// envelope is the on-disk and in-storage JSON wrapper around a slot's payload.
type envelope struct {
	Version int             `json:"version"`
	Data    json.RawMessage `json:"data"`
}

func parseEnvelope(payload []byte) (envelope, error) {
	if len(payload) == 0 || !json.Valid(payload) {
		return envelope{}, ErrCorrupt
	}
	var env envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return envelope{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if env.Version < 1 || len(env.Data) == 0 || !json.Valid(env.Data) {
		return envelope{}, ErrCorrupt
	}
	// A literal null (or a scalar) is never a legitimate save body; accepting it
	// would "load" an empty value and skip the backup that may be intact.
	data := bytes.TrimSpace(env.Data)
	if len(data) == 0 || (data[0] != '{' && data[0] != '[') {
		return envelope{}, ErrCorrupt
	}
	return env, nil
}

func isValidEnvelope(payload []byte) bool {
	_, err := parseEnvelope(payload)
	return err == nil
}

// unmarshalClean decodes data into a copy of *dst first so a failed unmarshal never leaves dst half-written before backup recovery runs.
func unmarshalClean(data []byte, dst any) error {
	rv := reflect.ValueOf(dst)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return json.Unmarshal(data, dst)
	}
	tmp := reflect.New(rv.Elem().Type())
	// Deep-copy the base value first: a shallow copy shares maps, slices and
	// pointers, which encoding/json mutates in place on the way to a failure.
	if base, err := json.Marshal(rv.Elem().Interface()); err == nil {
		if err := json.Unmarshal(base, tmp.Interface()); err == nil {
			rv.Elem().Set(tmp.Elem())
		}
	}
	if err := json.Unmarshal(data, tmp.Interface()); err != nil {
		return err
	}
	rv.Elem().Set(tmp.Elem())
	return nil
}

// validateSlot rejects empty names, path separators, traversal sequences, control characters and Windows-reserved filenames.
func validateSlot(slot string) error {
	if slot == "" || len(slot) > 200 {
		return fmt.Errorf("%w: %q", ErrInvalidSlot, slot)
	}
	if slot[0] == '.' || slot[len(slot)-1] == '.' || slot[0] == ' ' || slot[len(slot)-1] == ' ' {
		return fmt.Errorf("%w: %q", ErrInvalidSlot, slot)
	}
	if strings.Contains(slot, "..") {
		return fmt.Errorf("%w: %q", ErrInvalidSlot, slot)
	}
	for _, r := range slot {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: %q", ErrInvalidSlot, slot)
		}
		switch r {
		case '/', '\\', '<', '>', ':', '"', '|', '?', '*':
			return fmt.Errorf("%w: %q", ErrInvalidSlot, slot)
		}
	}
	if isWindowsReserved(slot) {
		return fmt.Errorf("%w: %q", ErrInvalidSlot, slot)
	}
	if strings.EqualFold(slot, settingsKey) {
		return fmt.Errorf("%w: %q is reserved for settings", ErrInvalidSlot, slot)
	}
	return nil
}

func isWindowsReserved(name string) bool {
	base, _, _ := strings.Cut(name, ".")
	base = strings.ToUpper(base)
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		d := base[3]
		return d >= '1' && d <= '9'
	}
	return false
}
