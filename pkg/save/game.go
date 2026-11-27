package save

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/nsgo-dev/game/pkg/loaderr"
)

// Part is one named subsystem of a game that can be saved into and restored from a save slot.
type Part interface {
	// SaveState returns a JSON-encodable snapshot of the part's current state.
	SaveState() (any, error)
	// LoadState replaces the part's state from raw JSON previously written by SaveState (after any per-part migrations).
	LoadState(raw json.RawMessage) error
}

// StagedPart is an optional interface a Part may implement so Game.Load can validate and decode
// raw JSON into staging state without mutating live state. Game.Load calls PrepareLoad on every
// StagedPart before applying any part; once every part succeeds, it calls each returned apply
// function in registration order.
type StagedPart interface {
	Part
	PrepareLoad(raw json.RawMessage) (apply func(), err error)
}

// Resetter is an optional interface a Part may implement to reset its state to its defaults when
// loading an older save slot that does not contain the part.
type Resetter interface {
	ResetState()
}

// PartOption configures a part registered with Game.Register.
type PartOption func(*partConfig)

type partConfig struct {
	migrations map[int]Migration
	hasDefault bool
	defaultVal any
}

// WithPartMigration registers fn to upgrade a part's raw JSON from version from to from+1.
func WithPartMigration(from int, fn Migration) PartOption {
	if from < 1 {
		panic(fmt.Sprintf("save: part migration version %d must be >= 1", from))
	}
	if fn == nil {
		panic("save: nil part migration")
	}
	return func(cfg *partConfig) {
		if cfg.migrations == nil {
			cfg.migrations = make(map[int]Migration)
		}
		cfg.migrations[from] = fn
	}
}

// WithPartMigrations registers each migration in m for a part, keyed by starting version (from to from+1).
func WithPartMigrations(m map[int]Migration) PartOption {
	return func(cfg *partConfig) {
		for from, fn := range m {
			WithPartMigration(from, fn)(cfg)
		}
	}
}

// WithPartDefault sets the JSON-encodable default value applied to a part when loading an older
// save slot that does not contain that part.
func WithPartDefault(v any) PartOption {
	return func(cfg *partConfig) {
		cfg.hasDefault = true
		cfg.defaultVal = v
	}
}

// PartEntry pairs a part schema version with a JSON-encodable value for EncodeParts.
type PartEntry struct {
	// Version is the part's schema version; values below 1 are treated as 1.
	Version int
	// Data is the JSON-encodable state of the part.
	Data any
}

type partEnvelope struct {
	Version int             `json:"version"`
	Data    json.RawMessage `json:"data"`
}

type gameSlotDoc struct {
	Parts map[string]partEnvelope `json:"parts"`
}

// EncodeParts encodes parts into the JSON object layout Game.Save writes and Game.Load reads
// ({"parts": {"<name>": {"version": N, "data": ...}}}). Use it in a Store-level Migration when
// upgrading a legacy flat save struct to a Game registry.
func EncodeParts(parts map[string]PartEntry) (json.RawMessage, error) {
	doc := gameSlotDoc{
		Parts: make(map[string]partEnvelope, len(parts)),
	}
	for name, entry := range parts {
		if name == "" {
			return nil, errors.New("save: EncodeParts: empty part name")
		}
		ver := entry.Version
		if ver <= 0 {
			ver = 1
		}
		raw, err := json.Marshal(entry.Data)
		if err != nil {
			return nil, fmt.Errorf("save: EncodeParts %q: %w", name, err)
		}
		doc.Parts[name] = partEnvelope{
			Version: ver,
			Data:    json.RawMessage(raw),
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("save: EncodeParts: %w", err)
	}
	return json.RawMessage(out), nil
}

type registeredPart struct {
	name       string
	version    int
	part       Part
	migrations map[int]Migration
	hasDefault bool
	defaultVal any
}

// Game holds an ordered registry of named saveable parts so a game can save and load all of its
// subsystems in one call, with per-part schema versions, per-part migrations and all-or-nothing loads.
//
// The zero value of Game is ready to use.
type Game struct {
	parts       []*registeredPart
	byName      map[string]*registeredPart
	pendingMigs map[string]map[int]Migration
}

// NewGame returns an empty Game save registry.
func NewGame() *Game {
	return &Game{}
}

// Register adds p to g under name with schema version version (versions below 1 are treated as 1).
//
// Parts are saved and applied in registration order. Register panics if name is empty, p is nil,
// or name is already registered.
func (g *Game) Register(name string, version int, p Part, opts ...PartOption) {
	if g == nil {
		panic("save: Register on nil Game")
	}
	if name == "" {
		panic("save: Register with empty part name")
	}
	if p == nil {
		panic(fmt.Sprintf("save: Register %q with nil Part", name))
	}
	if _, dup := g.byName[name]; dup {
		panic(fmt.Sprintf("save: part %q is already registered", name))
	}
	if version <= 0 {
		version = 1
	}
	var cfg partConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if pending := g.pendingMigs[name]; len(pending) > 0 {
		if cfg.migrations == nil {
			cfg.migrations = make(map[int]Migration, len(pending))
		}
		for from, fn := range pending {
			if _, exists := cfg.migrations[from]; !exists {
				cfg.migrations[from] = fn
			}
		}
		delete(g.pendingMigs, name)
	}
	rp := &registeredPart{
		name:       name,
		version:    version,
		part:       p,
		migrations: cfg.migrations,
		hasDefault: cfg.hasDefault,
		defaultVal: cfg.defaultVal,
	}
	if g.byName == nil {
		g.byName = make(map[string]*registeredPart)
	}
	g.byName[name] = rp
	g.parts = append(g.parts, rp)
}

// RegisterMigration registers fn to upgrade the named part's raw JSON from version from to from+1.
// It may be called before or after Register for part.
func (g *Game) RegisterMigration(part string, from int, fn Migration) {
	if g == nil {
		panic("save: RegisterMigration on nil Game")
	}
	if part == "" {
		panic("save: RegisterMigration with empty part name")
	}
	if from < 1 {
		panic(fmt.Sprintf("save: part %q migration version %d must be >= 1", part, from))
	}
	if fn == nil {
		panic(fmt.Sprintf("save: part %q migration from %d is nil", part, from))
	}
	if rp, ok := g.byName[part]; ok {
		if rp.migrations == nil {
			rp.migrations = make(map[int]Migration)
		}
		rp.migrations[from] = fn
		return
	}
	if g.pendingMigs == nil {
		g.pendingMigs = make(map[string]map[int]Migration)
	}
	if g.pendingMigs[part] == nil {
		g.pendingMigs[part] = make(map[int]Migration)
	}
	g.pendingMigs[part][from] = fn
}

// Parts returns the registered part names in registration order.
func (g *Game) Parts() []string {
	if g == nil || len(g.parts) == 0 {
		return nil
	}
	out := make([]string, len(g.parts))
	for i, rp := range g.parts {
		out[i] = rp.name
	}
	return out
}

// PartVersion returns the registered schema version for name and whether name is registered.
func (g *Game) PartVersion(name string) (int, bool) {
	if g == nil {
		return 0, false
	}
	rp, ok := g.byName[name]
	if !ok {
		return 0, false
	}
	return rp.version, true
}

// PartSnapshots captures every registered part's current state as compacted JSON, keyed by part name.
// Parts are snapshotted in registration order.
func (g *Game) PartSnapshots() (map[string]json.RawMessage, error) {
	if g == nil {
		return nil, errors.New("save: nil game")
	}
	out := make(map[string]json.RawMessage, len(g.parts))
	for _, rp := range g.parts {
		raw, err := marshalPart(rp)
		if err != nil {
			return nil, err
		}
		out[rp.name] = raw
	}
	return out, nil
}

// ResetParts calls ResetState on every registered part that implements Resetter, in registration order.
func (g *Game) ResetParts() {
	if g == nil {
		return
	}
	for _, rp := range g.parts {
		if r, ok := rp.part.(Resetter); ok {
			r.ResetState()
		}
	}
}

func marshalPart(rp *registeredPart) (json.RawMessage, error) {
	val, err := rp.part.SaveState()
	if err != nil {
		return nil, fmt.Errorf("save: part %q: %w", rp.name, err)
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return nil, fmt.Errorf("save: marshal part %q: %w", rp.name, err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err == nil {
		raw = compact.Bytes()
	}
	return json.RawMessage(raw), nil
}

// Save snapshots every registered part in registration order and writes them atomically to slot in s.
// If any part fails to snapshot or marshal, s is not touched and any previous save in slot stays intact.
func (g *Game) Save(s *Store, slot string) error {
	if g == nil {
		return errors.New("save: nil game")
	}
	if s == nil {
		return errors.New("save: nil store")
	}
	doc := gameSlotDoc{
		Parts: make(map[string]partEnvelope, len(g.parts)),
	}
	for _, rp := range g.parts {
		raw, err := marshalPart(rp)
		if err != nil {
			return err
		}
		doc.Parts[rp.name] = partEnvelope{
			Version: rp.version,
			Data:    raw,
		}
	}
	return s.Save(slot, doc)
}

type preparedAction struct {
	rp      *registeredPart
	kind    actionKind
	apply   func()
	raw     json.RawMessage
	resetFn func()
}

type actionKind uint8

const (
	actionNone actionKind = iota
	actionStaged
	actionPlain
	actionReset
)

// Load reads slot from s, validates and migrates every registered part before mutating any of them,
// and then applies all parts in registration order.
//
// If any part fails validation, migration or loading, none of the registered parts are changed.
// When a registered part is missing from an older slot, it is initialized from WithPartDefault when
// configured, reset via Resetter when implemented, or left unchanged otherwise. Unregistered parts
// present in the slot are ignored.
//
// Because Load runs through Store.LoadChecked, a primary copy that fails decoding or part validation
// automatically falls back to the slot's backup copy, while a future part version (ErrFutureVersion)
// or missing part migration (ErrMissingMigration) is returned directly without downgrading to backup.
func (g *Game) Load(s *Store, slot string) error {
	if g == nil {
		return errors.New("save: nil game")
	}
	if s == nil {
		return errors.New("save: nil store")
	}
	var rawDoc json.RawMessage
	return s.LoadChecked(slot, &rawDoc, func() error {
		savedParts, err := parseGameSlotDoc(rawDoc, g.byName)
		if err != nil {
			return err
		}
		actions := make([]preparedAction, 0, len(g.parts))
		hasPlain := false
		for _, rp := range g.parts {
			entry, present := savedParts[rp.name]
			if present {
				migrated, err := migratePart(rp, entry)
				if err != nil {
					return err
				}
				if sp, ok := rp.part.(StagedPart); ok {
					apply, err := sp.PrepareLoad(migrated)
					if err != nil {
						return wrapPartError(rp.name, "data", err)
					}
					actions = append(actions, preparedAction{rp: rp, kind: actionStaged, apply: apply})
				} else {
					hasPlain = true
					actions = append(actions, preparedAction{rp: rp, kind: actionPlain, raw: migrated})
				}
				continue
			}
			// Part was added after this slot was written.
			if rp.hasDefault {
				rawDef, err := json.Marshal(rp.defaultVal)
				if err != nil {
					return loaderr.Errorf("parts", "fix the registered default value for part "+rp.name, "marshal default for part %q: %w", rp.name, err)
				}
				if sp, ok := rp.part.(StagedPart); ok {
					apply, err := sp.PrepareLoad(json.RawMessage(rawDef))
					if err != nil {
						return wrapPartError(rp.name, "data", err)
					}
					actions = append(actions, preparedAction{rp: rp, kind: actionStaged, apply: apply})
				} else {
					hasPlain = true
					actions = append(actions, preparedAction{rp: rp, kind: actionPlain, raw: json.RawMessage(rawDef)})
				}
				continue
			}
			if r, ok := rp.part.(Resetter); ok {
				actions = append(actions, preparedAction{rp: rp, kind: actionReset, resetFn: r.ResetState})
			}
		}

		// Fast path: when every part to be applied is staged (or reset), none can fail during commit.
		if !hasPlain {
			for _, act := range actions {
				switch act.kind {
				case actionStaged:
					if act.apply != nil {
						act.apply()
					}
				case actionReset:
					act.resetFn()
				}
			}
			return nil
		}

		// Slow path: at least one plain Part exists whose LoadState may fail after earlier parts apply.
		// Capture pre-load snapshots of every part that will be mutated so a mid-load error rolls all back.
		rollback := make([]struct {
			rp  *registeredPart
			raw json.RawMessage
		}, len(actions))
		for i, act := range actions {
			prev, err := marshalPart(act.rp)
			if err != nil {
				return fmt.Errorf("save: snapshot part %q before load: %w", act.rp.name, err)
			}
			rollback[i].rp = act.rp
			rollback[i].raw = prev
		}

		for i, act := range actions {
			switch act.kind {
			case actionStaged:
				if act.apply != nil {
					act.apply()
				}
			case actionReset:
				act.resetFn()
			case actionPlain:
				if err := act.rp.part.LoadState(act.raw); err != nil {
					for j := i - 1; j >= 0; j-- {
						_ = rollback[j].rp.part.LoadState(rollback[j].raw)
					}
					return wrapPartError(act.rp.name, "data", err)
				}
			}
		}
		return nil
	})
}

// PeekPart reads and migrates one named part from slot in s and decodes it into dst without
// modifying any live part registered on g. It is intended for load-menu summaries and inspection.
//
// When name is not present in the slot, PeekPart returns ErrNotFound.
func (g *Game) PeekPart(s *Store, slot, name string, dst any) error {
	if g == nil {
		return errors.New("save: nil game")
	}
	if s == nil {
		return errors.New("save: nil store")
	}
	rp, ok := g.byName[name]
	if !ok {
		return fmt.Errorf("save: unknown part %q", name)
	}
	var rawDoc json.RawMessage
	var partRaw json.RawMessage
	found := false
	err := s.LoadChecked(slot, &rawDoc, func() error {
		savedParts, err := parseGameSlotDoc(rawDoc, g.byName)
		if err != nil {
			return err
		}
		entry, present := savedParts[name]
		if !present {
			found = false
			return nil
		}
		migrated, err := migratePart(rp, entry)
		if err != nil {
			return err
		}
		if err := unmarshalClean(migrated, dst); err != nil {
			return wrapPartError(name, "data", err)
		}
		partRaw = migrated
		found = true
		return nil
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("save: part %q in slot %q: %w", name, slot, ErrNotFound)
	}
	_ = partRaw
	return nil
}

func migratePart(rp *registeredPart, entry partEnvelope) (json.RawMessage, error) {
	if entry.Version > rp.version {
		return nil, &loaderr.Error{
			Field:    partField(rp.name, "version"),
			Problem:  fmt.Sprintf("part %q version %d exceeds current version %d", rp.name, entry.Version, rp.version),
			Expected: fmt.Sprintf("%d or lower", rp.version),
			Fix:      "load the save with the game version that wrote it, or a newer one",
			Err:      ErrFutureVersion,
		}
	}
	data := entry.Data
	for v := entry.Version; v < rp.version; v++ {
		var fn Migration
		if rp.migrations != nil {
			fn = rp.migrations[v]
		}
		if fn == nil {
			return nil, loaderr.Errorf(
				partField(rp.name, "version"),
				fmt.Sprintf("register a migration for part %q from version %d with WithPartMigration or RegisterMigration", rp.name, v),
				"part %q has no migration from version %d to %d: %w", rp.name, v, v+1, ErrMissingMigration,
			)
		}
		next, err := fn(data)
		if err != nil {
			return nil, loaderr.Errorf(
				partField(rp.name, "data"),
				fmt.Sprintf("fix the migration for part %q from version %d, or the data it refused", rp.name, v),
				"migrate part %q version %d to %d: %w", rp.name, v, v+1, err,
			)
		}
		if len(bytes.TrimSpace(next)) == 0 || !json.Valid(next) {
			return nil, loaderr.Errorf(
				partField(rp.name, "data"),
				fmt.Sprintf("make the migration for part %q from version %d return valid JSON", rp.name, v),
				"migration for part %q %d to %d returned invalid JSON: %w", rp.name, v, v+1, ErrCorrupt,
			)
		}
		data = next
	}
	return data, nil
}

func partField(name, sub string) string {
	return loaderr.Join("parts", name, sub)
}

func wrapPartError(name, sub string, err error) error {
	if e, ok := loaderr.As(err); ok {
		c := *e
		base := partField(name, sub)
		if c.Field == "" {
			c.Field = base
		} else if c.Field[0] == '[' {
			c.Field = base + c.Field
		} else {
			c.Field = base + "." + c.Field
		}
		return &c
	}
	return loaderr.Errorf(partField(name, sub), fixCorrupt, "part %q: %w", name, err)
}

// parseGameSlotDoc strictly validates raw as {"parts": {"<name>": {"version": N, "data": ...}}}.
// Unknown keys and duplicate keys at the game envelope or part envelope level are refused as ErrCorrupt,
// except that a registered part carrying a version newer than registered reports ErrFutureVersion even
// when it includes fields this build does not know.
func parseGameSlotDoc(raw json.RawMessage, registered map[string]*registeredPart) (map[string]partEnvelope, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, loaderr.Errorf("", fixCorrupt, "game save payload is not a JSON object: %w", ErrCorrupt)
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, loaderr.Errorf("", fixCorrupt, "game save payload is not a JSON object: %w", ErrCorrupt)
	}
	var seenParts bool
	var rawParts json.RawMessage
	for dec.More() {
		ktok, err := dec.Token()
		if err != nil {
			return nil, errors.Join(ErrCorrupt, err)
		}
		key, _ := ktok.(string)
		switch {
		case key == "parts" && !seenParts:
			seenParts = true
			if err := dec.Decode(&rawParts); err != nil {
				return nil, errors.Join(ErrCorrupt, err)
			}
		case key == "parts":
			return nil, loaderr.Errorf("parts", fixCorrupt, "%w: duplicate field %q", ErrCorrupt, key)
		default:
			return nil, loaderr.Errorf(key, fixCorrupt, "%w: unknown game save field %q", ErrCorrupt, key)
		}
	}
	if _, err := dec.Token(); err != nil {
		return nil, errors.Join(ErrCorrupt, err)
	}
	if !seenParts {
		return nil, loaderr.Errorf("parts", fixCorrupt, "missing \"parts\" object: %w", ErrCorrupt)
	}

	partsTrimmed := bytes.TrimSpace(rawParts)
	if len(partsTrimmed) == 0 || partsTrimmed[0] != '{' {
		return nil, loaderr.Errorf("parts", fixCorrupt, "\"parts\" is not a JSON object: %w", ErrCorrupt)
	}
	pdec := json.NewDecoder(bytes.NewReader(partsTrimmed))
	if tok, err := pdec.Token(); err != nil || tok != json.Delim('{') {
		return nil, loaderr.Errorf("parts", fixCorrupt, "\"parts\" is not a JSON object: %w", ErrCorrupt)
	}
	out := make(map[string]partEnvelope)
	for pdec.More() {
		ktok, err := pdec.Token()
		if err != nil {
			return nil, errors.Join(ErrCorrupt, err)
		}
		name, _ := ktok.(string)
		if name == "" {
			return nil, loaderr.Errorf("parts", fixCorrupt, "%w: empty part name", ErrCorrupt)
		}
		if _, dup := out[name]; dup {
			return nil, loaderr.Errorf(loaderr.Join("parts", name), fixCorrupt, "%w: duplicate part %q", ErrCorrupt, name)
		}
		var rawEntry json.RawMessage
		if err := pdec.Decode(&rawEntry); err != nil {
			return nil, errors.Join(ErrCorrupt, err)
		}
		var maxVer int
		if rp, ok := registered[name]; ok {
			maxVer = rp.version
		}
		entry, err := parsePartEnvelope(name, rawEntry, maxVer)
		if err != nil {
			return nil, err
		}
		out[name] = entry
	}
	if _, err := pdec.Token(); err != nil {
		return nil, errors.Join(ErrCorrupt, err)
	}
	return out, nil
}

func parsePartEnvelope(name string, raw json.RawMessage, maxVersion int) (partEnvelope, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return partEnvelope{}, loaderr.Errorf(loaderr.Join("parts", name), fixCorrupt, "part %q entry is not a JSON object: %w", name, ErrCorrupt)
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return partEnvelope{}, loaderr.Errorf(loaderr.Join("parts", name), fixCorrupt, "part %q entry is not a JSON object: %w", name, ErrCorrupt)
	}
	var env partEnvelope
	var seenVersion, seenData bool
	for dec.More() {
		ktok, err := dec.Token()
		if err != nil {
			return partEnvelope{}, errors.Join(ErrCorrupt, err)
		}
		key, _ := ktok.(string)
		switch {
		case key == "version" && !seenVersion:
			seenVersion = true
			if err := dec.Decode(&env.Version); err != nil {
				return partEnvelope{}, loaderr.Errorf(partField(name, "version"), fixCorrupt, "part %q version: %w: %w", name, ErrCorrupt, err)
			}
		case key == "data" && !seenData:
			seenData = true
			if err := dec.Decode(&env.Data); err != nil {
				return partEnvelope{}, loaderr.Errorf(partField(name, "data"), fixCorrupt, "part %q data: %w: %w", name, ErrCorrupt, err)
			}
		case key == "version" || key == "data":
			return partEnvelope{}, loaderr.Errorf(partField(name, key), fixCorrupt, "%w: duplicate field %q in part %q", ErrCorrupt, key, name)
		default:
			if maxVersion > 0 {
				if v, ok := EnvelopeVersion(trimmed); ok && v > maxVersion {
					return partEnvelope{}, &loaderr.Error{
						Field:    partField(name, "version"),
						Problem:  fmt.Sprintf("part %q version %d exceeds current version %d", name, v, maxVersion),
						Expected: fmt.Sprintf("%d or lower", maxVersion),
						Fix:      "load the save with the game version that wrote it, or a newer one",
						Err:      ErrFutureVersion,
					}
				}
			}
			return partEnvelope{}, loaderr.Errorf(partField(name, key), fixCorrupt, "%w: unknown field %q in part %q", ErrCorrupt, key, name)
		}
	}
	if _, err := dec.Token(); err != nil {
		return partEnvelope{}, errors.Join(ErrCorrupt, err)
	}
	if !seenVersion || env.Version < 1 {
		return partEnvelope{}, loaderr.Errorf(partField(name, "version"), fixCorrupt, "part %q version %d is not 1 or more: %w", name, env.Version, ErrCorrupt)
	}
	if !seenData || len(bytes.TrimSpace(env.Data)) == 0 {
		return partEnvelope{}, loaderr.Errorf(partField(name, "data"), fixCorrupt, "part %q data is missing: %w", name, ErrCorrupt)
	}
	return env, nil
}

// ValuePart returns a StagedPart (and Resetter) that saves and loads the JSON-encodable value at ptr.
// Loading unmarshals into a fresh T before writing *ptr, and ResetState zeroes *ptr when an older
// save slot omits the part.
func ValuePart[T any](ptr *T) StagedPart {
	if ptr == nil {
		panic("save: ValuePart with nil pointer")
	}
	return &valuePart[T]{ptr: ptr}
}

type valuePart[T any] struct {
	ptr *T
}

func (v *valuePart[T]) SaveState() (any, error) {
	return *v.ptr, nil
}

func (v *valuePart[T]) LoadState(raw json.RawMessage) error {
	apply, err := v.PrepareLoad(raw)
	if err != nil {
		return err
	}
	apply()
	return nil
}

func (v *valuePart[T]) PrepareLoad(raw json.RawMessage) (func(), error) {
	var next T
	if err := json.Unmarshal(raw, &next); err != nil {
		return nil, err
	}
	return func() { *v.ptr = next }, nil
}

func (v *valuePart[T]) ResetState() {
	var zero T
	*v.ptr = zero
}

// FuncPart adapts a typed save/load callback pair into a Part.
// JSON decoding happens before loadFn runs, and Game.Load snapshots and rolls back automatically if
// a later part fails. When resetFn is provided, the part also implements Resetter.
func FuncPart[T any](saveFn func() (T, error), loadFn func(T) error, resetFn ...func()) Part {
	if saveFn == nil || loadFn == nil {
		panic("save: FuncPart requires non-nil saveFn and loadFn")
	}
	var reset func()
	if len(resetFn) > 0 {
		reset = resetFn[0]
	}
	if reset != nil {
		return &resetFuncPart[T]{funcPart: funcPart[T]{saveFn: saveFn, loadFn: loadFn}, resetFn: reset}
	}
	return &funcPart[T]{saveFn: saveFn, loadFn: loadFn}
}

type funcPart[T any] struct {
	saveFn func() (T, error)
	loadFn func(T) error
}

func (f *funcPart[T]) SaveState() (any, error) {
	return f.saveFn()
}

func (f *funcPart[T]) LoadState(raw json.RawMessage) error {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	return f.loadFn(v)
}

type resetFuncPart[T any] struct {
	funcPart[T]
	resetFn func()
}

func (r *resetFuncPart[T]) ResetState() {
	r.resetFn()
}

// StagedFuncPart adapts a typed save/prepare callback pair into a StagedPart.
// prepareFn validates the decoded T without mutating live state and returns an apply function
// invoked only after every part in Game.Load has passed validation. When resetFn is provided,
// the returned part also implements Resetter.
func StagedFuncPart[T any](saveFn func() (T, error), prepareFn func(T) (func(), error), resetFn ...func()) StagedPart {
	if saveFn == nil || prepareFn == nil {
		panic("save: StagedFuncPart requires non-nil saveFn and prepareFn")
	}
	var reset func()
	if len(resetFn) > 0 {
		reset = resetFn[0]
	}
	if reset != nil {
		return &resetStagedFuncPart[T]{stagedFuncPart: stagedFuncPart[T]{saveFn: saveFn, prepareFn: prepareFn}, resetFn: reset}
	}
	return &stagedFuncPart[T]{saveFn: saveFn, prepareFn: prepareFn}
}

type stagedFuncPart[T any] struct {
	saveFn    func() (T, error)
	prepareFn func(T) (func(), error)
}

func (f *stagedFuncPart[T]) SaveState() (any, error) {
	return f.saveFn()
}

func (f *stagedFuncPart[T]) LoadState(raw json.RawMessage) error {
	apply, err := f.PrepareLoad(raw)
	if err != nil {
		return err
	}
	if apply != nil {
		apply()
	}
	return nil
}

func (f *stagedFuncPart[T]) PrepareLoad(raw json.RawMessage) (func(), error) {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return f.prepareFn(v)
}

type resetStagedFuncPart[T any] struct {
	stagedFuncPart[T]
	resetFn func()
}

func (r *resetStagedFuncPart[T]) ResetState() {
	r.resetFn()
}

// EqualJSON reports whether a and b represent the same JSON value, ignoring whitespace and object
// key ordering differences.
func EqualJSON(a, b json.RawMessage) bool {
	if bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b)) {
		return true
	}
	var va, vb any
	da := json.NewDecoder(bytes.NewReader(a))
	da.UseNumber()
	if err := da.Decode(&va); err != nil {
		return false
	}
	db := json.NewDecoder(bytes.NewReader(b))
	db.UseNumber()
	if err := db.Decode(&vb); err != nil {
		return false
	}
	return equalJSONValue(va, vb)
}

func equalJSONValue(a, b any) bool {
	switch av := a.(type) {
	case nil:
		return b == nil
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case json.Number:
		bv, ok := b.(json.Number)
		if !ok {
			return false
		}
		if av == bv {
			return true
		}
		af, aerr := av.Float64()
		bf, berr := bv.Float64()
		return aerr == nil && berr == nil && af == bf
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !equalJSONValue(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			ov, exists := bv[k]
			if !exists || !equalJSONValue(v, ov) {
				return false
			}
		}
		return true
	default:
		return slices.Equal([]string{fmt.Sprint(a)}, []string{fmt.Sprint(b)})
	}
}
