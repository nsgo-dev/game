// Package save stores a game's save slots as versioned JSON in either the app's key-value storage (core.Storage) or files in a directory such as cx.DataDir.
//
// Each slot holds a {"version": N, "data": ...} envelope; an envelope with any other field, or with a field given twice, is corrupt. When a Store loads a save written by an older version, it runs its registered migrations one version at a time (v to v+1) before unmarshaling into the caller's value; a save from a newer version is refused with ErrFutureVersion rather than read into an older shape.
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

	"github.com/nsgo-dev/game/pkg/loaderr"
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
// If the primary copy is missing, unreadable or corrupted, Load automatically recovers from the slot's backup copy. It rewrites the primary from that backup only when the primary is missing or is not a JSON object at all (empty, truncated or garbage); a primary that failed to read for another reason, or that is a JSON object Load cannot use, is left as it is, because it may be newer than the backup. A save whose version exceeds s.Version returns an error wrapping ErrFutureVersion without falling back to an older backup, even when its envelope has fields this build does not know.
//
// Load never writes a migrated save back: migrations run in memory on every Load until the slot is saved again, so each Migration must be a pure function of its input that gives the same result every time it runs.
//
// An error about a copy's contents is a *loaderr.Error that names the copy, the line in the stored JSON and a fix. FileBackend names it by its file path (<dir>/<slot>.json, or <dir>/<slot>.json.bak for the backup) and StorageBackend by its storage key; another Backend by the slot, with " (backup)" for its backup copy.
func (s *Store) Load(slot string, dst any) error {
	return s.LoadChecked(slot, dst, nil)
}

// LoadChecked is Load with a check of the decoded value: after a copy decodes into dst, LoadChecked calls check, and a copy check refuses is treated as corrupted, so the backup is tried as it would be for one that does not decode.
//
// Before the backup is decoded, and when LoadChecked returns an error, dst is put back to the value it had when LoadChecked was called, so nothing of a refused copy is left in it.
//
// When check returns a *loaderr.Error whose Field is a path in dst's JSON, the error LoadChecked returns gives that field's line in the stored copy, under "data". A nil check checks nothing.
func (s *Store) LoadChecked(slot string, dst any, check func() error) error {
	if err := validateSlot(slot); err != nil {
		return err
	}
	if s.backend == nil {
		return errors.New("save: store has no backend")
	}
	restore := snapshot(dst)
	decode := func(backup bool, payload []byte) error {
		err := s.decode(copyName(s.backend, slot, backup), payload, dst, check)
		if err != nil {
			restore()
		}
		return err
	}
	primary, primaryErr := s.backend.Read(slot)
	if primaryErr == nil {
		err := decode(false, primary)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrFutureVersion) || errors.Is(err, ErrMissingMigration) {
			return err
		}
		if backup, bakErr := s.backend.ReadBackup(slot); bakErr == nil {
			recErr := decode(true, backup)
			if recErr == nil {
				if isDamagedPayload(primary) {
					s.repair(slot, backup)
				}
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
	if err := decode(true, backup); err != nil {
		return err
	}
	// Only a primary that is really gone is rewritten: a read that failed for another reason (permissions, I/O, too many open files) may be hiding a valid primary newer than this backup (#546).
	if errors.Is(primaryErr, ErrNotFound) {
		s.repair(slot, backup)
	}
	return nil
}

// snapshot records the value dst points at and returns a func that puts a copy of that value back into dst. The copy is deep, as unmarshalClean's base is, so decoding into dst again cannot change what is put back. For a dst that is not a non-nil pointer it returns a func that does nothing.
func snapshot(dst any) func() {
	rv := reflect.ValueOf(dst)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return func() {}
	}
	t := rv.Elem().Type()
	base, err := json.Marshal(rv.Elem().Interface())
	if err != nil {
		// A value JSON cannot encode is put back as it was, sharing its maps and slices with what decoding left.
		orig := reflect.New(t).Elem()
		orig.Set(rv.Elem())
		return func() { rv.Elem().Set(orig) }
	}
	return func() {
		fresh := reflect.New(t)
		if json.Unmarshal(base, fresh.Interface()) == nil {
			rv.Elem().Set(fresh.Elem())
		}
	}
}

// repair rewrites a missing or damaged primary copy from the backup Load just recovered, so the slot is whole again instead of depending on the backup until the next Save (#546). The backend keeps the backup, because Write preserves only a valid primary. Repair is best effort: Load has already succeeded, and a failed rewrite leaves the slot exactly as it was.
func (s *Store) repair(slot string, backup []byte) {
	_ = s.backend.Write(slot, backup)
}

// Delete removes slot and its backup copy. Deleting a slot that does not exist returns nil. The backup goes first, so an interrupted Delete leaves either the whole slot or nothing that Load could resurrect.
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

// backupName names a slot's backup copy in errors when its backend has no name for it.
func backupName(slot string) string { return slot + " (backup)" }

// copyNamer is implemented by the backends this package provides, to name a copy of a slot in errors by the file path or storage key that holds it, which an editor can open or a developer can look up.
type copyNamer interface {
	copyName(slot string, backup bool) string
}

// copyName names the primary or backup copy of slot in b for errors: b's own name for it when b has one, else the slot, with " (backup)" for its backup.
func copyName(b any, slot string, backup bool) string {
	if n, ok := b.(copyNamer); ok {
		return n.copyName(slot, backup)
	}
	if backup {
		return backupName(slot)
	}
	return slot
}

// decode decodes payload, the copy of a save named name, into dst.
func (s *Store) decode(name string, payload []byte, dst any, check func() error) error {
	return decodeEnvelope(name, payload, s.version(), s.Migrations, dst, check)
}

// decodeEnvelope decodes payload, the copy of a save named name, into dst. Its errors are *loaderr.Error values that name name and the line of the problem in payload, and match ErrCorrupt, ErrFutureVersion or ErrMissingMigration with errors.Is.
//
// When check is not nil, it runs after dst is decoded, and a *loaderr.Error it returns about a field of dst is reported at that field under "data".
func decodeEnvelope(name string, payload []byte, target int, migrations map[int]Migration, dst any, check func() error) error {
	return loaderr.Locate(decodeEnvelopeData(payload, target, migrations, dst, check), name, payload, fixCorrupt)
}

// futureVersion is the error for a save copy whose version, v, is newer than target, the version this build reads. It matches ErrFutureVersion with errors.Is.
func futureVersion(v, target int) error {
	return &loaderr.Error{Field: "version", Problem: fmt.Sprintf("save version %d exceeds current version %d", v, target), Expected: fmt.Sprintf("%d or lower", target), Fix: "load the save with the game version that wrote it, or a newer one", Err: ErrFutureVersion}
}

// fixCorrupt is the fix for a save copy that cannot be read.
const fixCorrupt = "the copy is damaged or was not written by this game; delete the slot to start over, or restore the file from a backup"

func decodeEnvelopeData(payload []byte, target int, migrations map[int]Migration, dst any, check func() error) error {
	env, err := parseEnvelope(payload)
	if err != nil {
		// An envelope a newer build wrote may carry fields this one does not know; report it as a future version rather than as corrupt, so Load does not fall back to an older backup.
		var lenient struct {
			Version int `json:"version"`
		}
		if json.Unmarshal(payload, &lenient) == nil && lenient.Version > target {
			return futureVersion(lenient.Version, target)
		}
		return err
	}
	if env.Version > target {
		return futureVersion(env.Version, target)
	}
	data := env.Data
	migrated := env.Version < target
	for v := env.Version; v < target; v++ {
		var fn Migration
		if migrations != nil {
			fn = migrations[v]
		}
		if fn == nil {
			return loaderr.Errorf("version", fmt.Sprintf("register a migration from version %d with RegisterMigration", v), "no migration from version %d to %d: %w", v, v+1, ErrMissingMigration)
		}
		next, err := fn(data)
		if err != nil {
			return loaderr.Errorf("data", fmt.Sprintf("fix the migration from version %d, or the data it refused", v), "migrate version %d to %d: %w", v, v+1, err)
		}
		if len(next) == 0 || !json.Valid(next) {
			return loaderr.Errorf("data", fmt.Sprintf("make the migration from version %d return valid JSON", v), "migration %d to %d returned invalid JSON: %w", v, v+1, ErrCorrupt)
		}
		data = next
	}
	if err := unmarshalClean(data, dst); err != nil {
		field := "data"
		var typ *json.UnmarshalTypeError
		if !migrated && errors.As(err, &typ) && typ.Field != "" {
			if e, ok := loaderr.As(loaderr.Locate(err, "", data, "")); ok {
				field = underData(e.Field)
			}
		}
		return loaderr.Errorf(field, "write the value the game's save type expects here, or delete the slot to start over", "unmarshal: %w", err)
	}
	if check != nil {
		if err := check(); err != nil {
			e, ok := loaderr.As(err)
			if !ok {
				return loaderr.Errorf("data", fixCorrupt, "%w", err)
			}
			c := *e
			// Migrated data is not the payload's text, so its fields have no line there.
			if migrated {
				c.Field = "data"
			} else {
				c.Field = underData(e.Field)
			}
			if c.Fix == "" {
				c.Fix = fixCorrupt
			}
			return &c
		}
	}
	return nil
}

// underData returns the path of field, a path in a save's data, in the whole envelope, where data sits under "data".
func underData(field string) string {
	switch {
	case field == "":
		return "data"
	case field[0] == '[':
		return "data" + field
	}
	return "data." + field
}

// envelope is the on-disk and in-storage JSON wrapper around a slot's payload.
type envelope struct {
	Version int             `json:"version"`
	Data    json.RawMessage `json:"data"`
}

func parseEnvelope(payload []byte) (envelope, error) {
	if len(payload) == 0 {
		return envelope{}, loaderr.Errorf("", fixCorrupt, "empty save: %w", ErrCorrupt)
	}
	if err := checkEnvelopeKeys(payload); err != nil {
		return envelope{}, err
	}
	var env envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		// Join keeps the decode error, which loaderr.Locate reads for its line, beside ErrCorrupt.
		return envelope{}, errors.Join(ErrCorrupt, err)
	}
	if env.Version < 1 {
		return envelope{}, loaderr.Errorf("version", fixCorrupt, "save version %d is not 1 or more: %w", env.Version, ErrCorrupt)
	}
	// A literal null (or a scalar) is never a legitimate save body; accepting it
	// would "load" an empty value and skip the backup that may be intact.
	data := bytes.TrimSpace(env.Data)
	if len(data) == 0 || (data[0] != '{' && data[0] != '[') {
		return envelope{}, loaderr.Errorf("data", fixCorrupt, "save data is missing or not an object or array: %w", ErrCorrupt)
	}
	return env, nil
}

// checkEnvelopeKeys refuses an envelope with a key other than "version" and "data" or with a key given twice, which encoding/json would silently ignore or resolve to the last value (#546). A payload that is not a JSON object is left to json.Unmarshal, whose error gives the line.
func checkEnvelopeKeys(payload []byte) error {
	dec := json.NewDecoder(bytes.NewReader(payload))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		// json.Unmarshal, which parseEnvelope runs next, reports what is wrong and where.
		return nil
	}
	var seenVersion, seenData bool
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil
		}
		key, _ := tok.(string)
		switch {
		case key == "version" && !seenVersion:
			seenVersion = true
		case key == "data" && !seenData:
			seenData = true
		case key == "version" || key == "data":
			return loaderr.Errorf(key, fixCorrupt, "%w: duplicate envelope field %q", ErrCorrupt, key)
		default:
			return loaderr.Errorf(key, fixCorrupt, "%w: unknown envelope field %q", ErrCorrupt, key)
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil
		}
	}
	return nil
}

// isValidEnvelope reports whether payload is an envelope this build can parse; backends keep only such a primary as the backup when a new save replaces it.
func isValidEnvelope(payload []byte) bool {
	_, err := parseEnvelope(payload)
	return err == nil
}

// isDamagedPayload reports whether payload is not a JSON object at all: empty, truncated or garbage. Only such a primary is rewritten from the backup; a JSON object Load cannot use may come from a newer build and is left alone.
func isDamagedPayload(payload []byte) bool {
	p := bytes.TrimSpace(payload)
	return len(p) == 0 || p[0] != '{' || !json.Valid(p)
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

// ValidateSlot reports whether slot is a name a Store accepts, returning an error wrapping ErrInvalidSlot when it is not. Code that takes slot names from outside the game, such as a cloud listing, checks them with it before handing them to a Backend.
func ValidateSlot(slot string) error { return validateSlot(slot) }

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
