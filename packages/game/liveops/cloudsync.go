package liveops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/nsgo/core"
)

// CloudSaveFormat is the "format" value that marks a cloud payload written by CloudSyncer. A payload without it is read as a plain save written by a game that used CloudSync directly, at revision 0.
const CloudSaveFormat = "nsgo-cloud-save"

// CloudSaveVersion is the schema version of the cloud payload CloudSyncer writes and the newest one it reads.
const CloudSaveVersion = 1

// DefaultSyncStateKey is the core.Storage key prefix under which CloudSyncer keeps each slot's sync state when CloudSyncOptions.StateKey is empty.
const DefaultSyncStateKey = "liveops.cloudsync"

const syncStateVersion = 1

var (
	// ErrCorruptCloudSave reports a cloud payload marked with CloudSaveFormat that does not decode: an unknown field, a newer schema version, a hash that does not match its data, or a malformed value. The slot is left untouched on both sides.
	ErrCorruptCloudSave = errors.New("liveops: corrupt cloud save")
	// ErrCorruptSyncState reports a slot's stored sync state that does not decode. The slot is left untouched on both sides.
	ErrCorruptSyncState = errors.New("liveops: corrupt cloud sync state")
	// ErrCloudChanged reports that the cloud copy of a slot changed between CloudSyncer reading it and writing it, such as when another device uploaded at the same moment. Nothing was written; the next sync sees the new copy.
	ErrCloudChanged = errors.New("liveops: cloud save changed during sync")
	// ErrInvalidDevice reports a device ID that is empty, longer than 64 bytes or not made of ASCII letters, digits, '-' and '_'.
	ErrInvalidDevice = errors.New("liveops: invalid device id")
)

// ConflictPolicy chooses how CloudSyncer resolves a slot whose local and cloud copies both changed since the last sync.
type ConflictPolicy int

const (
	// PolicyNewest keeps the copy with the later modified time and overwrites the other. When either time is unknown or both are equal it keeps both, as PolicyKeepBoth does, and when one side deleted the slot and the other changed it, it keeps the changed copy.
	PolicyNewest ConflictPolicy = iota
	// PolicyKeepBoth keeps the cloud copy in the slot and moves the local copy into a new fork slot on both sides, so neither is lost. When one side deleted the slot and the other changed it, it keeps the changed copy.
	PolicyKeepBoth
	// PolicyAsk passes each conflict, with a summary of both copies, to CloudSyncOptions.Ask and applies the player's Choice.
	PolicyAsk
)

// String returns "newest", "keep-both", "ask" or "unknown".
func (p ConflictPolicy) String() string {
	switch p {
	case PolicyNewest:
		return "newest"
	case PolicyKeepBoth:
		return "keep-both"
	case PolicyAsk:
		return "ask"
	default:
		return "unknown"
	}
}

// Choice is the answer to a Conflict under PolicyAsk.
type Choice int

const (
	// ChoiceDefer leaves the conflict unresolved: nothing is written on either side and the next sync asks again.
	ChoiceDefer Choice = iota
	// ChoiceLocal keeps this device's copy and overwrites (or deletes) the cloud copy.
	ChoiceLocal
	// ChoiceCloud keeps the cloud copy and overwrites (or deletes) this device's copy; the local save backend keeps the overwritten copy as its backup.
	ChoiceCloud
	// ChoiceBoth keeps the cloud copy in the slot and moves this device's copy into a fork slot on both sides. When one side deleted the slot it keeps the copy that exists.
	ChoiceBoth
)

// String returns "defer", "local", "cloud", "both" or "unknown".
func (c Choice) String() string {
	switch c {
	case ChoiceDefer:
		return "defer"
	case ChoiceLocal:
		return "local"
	case ChoiceCloud:
		return "cloud"
	case ChoiceBoth:
		return "both"
	default:
		return "unknown"
	}
}

// SyncOutcome says what CloudSyncer did to one slot.
type SyncOutcome int

const (
	// OutcomeUpToDate means both sides already held the same copy.
	OutcomeUpToDate SyncOutcome = iota
	// OutcomeUploaded means only the local copy had changed, and it was uploaded.
	OutcomeUploaded
	// OutcomeDownloaded means only the cloud copy had changed, and it was written locally.
	OutcomeDownloaded
	// OutcomeDeletedCloud means the slot was deleted locally and unchanged in the cloud, so the cloud copy was deleted.
	OutcomeDeletedCloud
	// OutcomeDeletedLocal means the slot was deleted in the cloud and unchanged locally, so the local copy was deleted.
	OutcomeDeletedLocal
	// OutcomeKeptLocal means a conflict was resolved in favour of the local copy; SlotResult.Discarded holds the cloud copy it replaced.
	OutcomeKeptLocal
	// OutcomeKeptCloud means a conflict was resolved in favour of the cloud copy; SlotResult.Discarded holds the local copy it replaced, which the local save backend also keeps as its backup.
	OutcomeKeptCloud
	// OutcomeKeptBoth means a conflict was resolved by keeping the cloud copy in the slot and moving the local copy to SlotResult.Fork on both sides.
	OutcomeKeptBoth
	// OutcomeDeferred means a conflict was left unresolved under PolicyAsk; nothing was written.
	OutcomeDeferred
)

// String returns a lowercase, hyphenated name such as "up-to-date" or "kept-both".
func (o SyncOutcome) String() string {
	switch o {
	case OutcomeUpToDate:
		return "up-to-date"
	case OutcomeUploaded:
		return "uploaded"
	case OutcomeDownloaded:
		return "downloaded"
	case OutcomeDeletedCloud:
		return "deleted-cloud"
	case OutcomeDeletedLocal:
		return "deleted-local"
	case OutcomeKeptLocal:
		return "kept-local"
	case OutcomeKeptCloud:
		return "kept-cloud"
	case OutcomeKeptBoth:
		return "kept-both"
	case OutcomeDeferred:
		return "deferred"
	default:
		return "unknown"
	}
}

// SaveVersion describes one side's copy of a slot in a Conflict or a SlotResult.
type SaveVersion struct {
	// Exists is false when this side deleted the slot; the other fields are then zero.
	Exists bool
	// Revision is the cloud revision: the cloud copy's own, or for the local copy the revision it was last synced at (0 when never synced).
	Revision int64
	// Hash is "sha256:" followed by the hex SHA-256 of Data.
	Hash string
	// Modified is when the copy was last written, zero when unknown (a local write that bypassed CloudSyncer.Backend, or a plain cloud payload).
	Modified time.Time
	// Device is the ID of the device that wrote the copy, empty when unknown.
	Device string
	// Summary is CloudSyncOptions.Summarize's text for Data, such as "Level 12, 3h 40m played".
	Summary string
	// Data is the raw save payload.
	Data []byte
}

// Conflict is a slot whose local and cloud copies both changed since this device last synced it, passed to CloudSyncOptions.Ask under PolicyAsk.
type Conflict struct {
	// Slot is the save slot name.
	Slot string
	// Local is this device's copy.
	Local SaveVersion
	// Cloud is the copy in cloud storage.
	Cloud SaveVersion
}

// SlotResult reports what a sync did to one slot.
type SlotResult struct {
	// Slot is the save slot name.
	Slot string
	// Outcome is what was done.
	Outcome SyncOutcome
	// Conflict is set when both copies had changed, whatever the outcome.
	Conflict *Conflict
	// Fork is the slot the local copy was moved to under OutcomeKeptBoth.
	Fork string
	// Discarded is the copy a resolution replaced under OutcomeKeptLocal or OutcomeKeptCloud, so a game can show or archive it.
	Discarded *SaveVersion
}

// SyncResult reports what CloudSyncer.Sync did to each slot, sorted by slot name.
type SyncResult struct {
	// Slots holds one entry for each slot that synced without error.
	Slots []SlotResult
}

// Conflicts returns the slot results that held a conflict.
func (r SyncResult) Conflicts() []SlotResult {
	var out []SlotResult
	for _, s := range r.Slots {
		if s.Conflict != nil {
			out = append(out, s)
		}
	}
	return out
}

// CloudSyncOptions configures NewCloudSyncer.
type CloudSyncOptions struct {
	// Local is this device's save backend, such as save.StorageBackend or save.FileBackend. Required.
	Local save.Backend
	// Cloud is the player's cloud storage. Required.
	Cloud CloudSync
	// State holds each slot's sync state on this device, as JSON text under StateKey. Required.
	State core.Storage
	// StateKey is the key prefix CloudSyncer owns in State; DefaultSyncStateKey when empty.
	StateKey string
	// Device identifies this device in cloud payloads and fork slot names: 1 to 64 ASCII letters, digits, '-' or '_'. Required.
	Device string
	// Policy resolves conflicts; PolicyNewest when zero.
	Policy ConflictPolicy
	// Ask answers each conflict under PolicyAsk, typically by showing the player both summaries. It is required with PolicyAsk. An error leaves the slot untouched and is returned by the sync.
	Ask func(ctx context.Context, c Conflict) (Choice, error)
	// Summarize returns a short, player-facing description of a save payload for Conflict summaries. Optional.
	Summarize func(slot string, data []byte) string
	// Now returns the current time, used to stamp local writes made through Backend; time.Now when nil.
	Now func() time.Time
}

// CloudSyncer reconciles a device's save slots with cloud storage, detecting slots that changed on both sides since the last sync and resolving them by policy without silently losing either copy.
//
// It remembers, per slot, the content hash and cloud revision both sides agreed on at the last sync (the base). On each sync it compares the local copy, the cloud copy and the base: when only one side differs from the base that side is copied to the other, and when both differ (and from each other) the slot is a conflict. Cloud payloads are JSON text carrying the revision, content hash, modified time and writing device next to the save data.
//
// Write saves through Backend so that each local write is stamped with its time for PolicyNewest and conflict summaries. A CloudSyncer is not safe for concurrent use; call it where the app's code runs, as core.Storage requires.
type CloudSyncer struct {
	local     save.Backend
	cloud     CloudSync
	state     core.Storage
	prefix    string
	device    string
	policy    ConflictPolicy
	ask       func(context.Context, Conflict) (Choice, error)
	summarize func(string, []byte) string
	now       func() time.Time
}

// NewCloudSyncer returns a CloudSyncer for opts, or an error when a required option is missing or invalid.
func NewCloudSyncer(opts CloudSyncOptions) (*CloudSyncer, error) {
	if opts.Local == nil {
		return nil, errors.New("liveops: cloud syncer needs a local save backend")
	}
	if opts.Cloud == nil {
		return nil, errors.New("liveops: cloud syncer needs a CloudSync")
	}
	if opts.State == (core.Storage{}) {
		return nil, errors.New("liveops: cloud syncer needs a core.Storage for sync state")
	}
	if err := validateDevice(opts.Device); err != nil {
		return nil, err
	}
	switch opts.Policy {
	case PolicyNewest, PolicyKeepBoth:
	case PolicyAsk:
		if opts.Ask == nil {
			return nil, errors.New("liveops: PolicyAsk needs an Ask callback")
		}
	default:
		return nil, fmt.Errorf("liveops: unknown conflict policy %d", int(opts.Policy))
	}
	s := &CloudSyncer{
		local:     opts.Local,
		cloud:     opts.Cloud,
		state:     opts.State,
		prefix:    opts.StateKey,
		device:    opts.Device,
		policy:    opts.Policy,
		ask:       opts.Ask,
		summarize: opts.Summarize,
		now:       opts.Now,
	}
	if s.prefix == "" {
		s.prefix = DefaultSyncStateKey
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s, nil
}

func validateDevice(id string) error {
	if id == "" || len(id) > 64 {
		return fmt.Errorf("%w: %q", ErrInvalidDevice, id)
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return fmt.Errorf("%w: %q", ErrInvalidDevice, id)
		}
	}
	return nil
}

// Backend returns the local save backend wrapped so that every Write and Delete is stamped with the current time in the slot's sync state. Pass it to save.NewStore so PolicyNewest and conflict summaries know when each local copy was written.
func (s *CloudSyncer) Backend() save.Backend { return trackedBackend{s} }

type trackedBackend struct{ s *CloudSyncer }

func (b trackedBackend) Read(slot string) ([]byte, error)       { return b.s.local.Read(slot) }
func (b trackedBackend) ReadBackup(slot string) ([]byte, error) { return b.s.local.ReadBackup(slot) }
func (b trackedBackend) Exists(slot string) bool                { return b.s.local.Exists(slot) }
func (b trackedBackend) Slots() ([]string, error)               { return b.s.local.Slots() }

func (b trackedBackend) Write(slot string, data []byte) error {
	if err := b.s.local.Write(slot, data); err != nil {
		return err
	}
	return b.s.stampLocal(slot)
}

func (b trackedBackend) Delete(slot string) error {
	if err := b.s.local.Delete(slot); err != nil {
		return err
	}
	return b.s.stampLocal(slot)
}

func (s *CloudSyncer) stampLocal(slot string) error {
	st, err := s.loadState(slot)
	if err != nil {
		// A corrupt record must not block saving; start it over with only the stamp, which turns the next sync of this slot into a first sync.
		st = syncState{}
	}
	st.LocalModified = formatTime(s.now())
	return s.storeState(slot, st)
}

// Sync reconciles every slot known locally, in the cloud or in the sync state, in slot-name order.
//
// A slot that fails is skipped and its error joined into the returned error while the others still sync, except when the cloud is unavailable or ctx is done, which stops the sync.
func (s *CloudSyncer) Sync(ctx context.Context) (SyncResult, error) {
	var res SyncResult
	slots, err := s.allSlots(ctx)
	if err != nil {
		return res, err
	}
	var errs []error
	for _, slot := range slots {
		if err := ctx.Err(); err != nil {
			return res, errors.Join(append(errs, err)...)
		}
		r, err := s.SyncSlot(ctx, slot)
		if err != nil {
			errs = append(errs, err)
			if errors.Is(err, ErrCloudUnavailable) || ctx.Err() != nil {
				return res, errors.Join(errs...)
			}
			continue
		}
		res.Slots = append(res.Slots, r)
	}
	return res, errors.Join(errs...)
}

func (s *CloudSyncer) allSlots(ctx context.Context) ([]string, error) {
	seen := map[string]struct{}{}
	local, err := s.local.Slots()
	if err != nil {
		return nil, fmt.Errorf("liveops: list local saves: %w", err)
	}
	cloud, err := s.cloud.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("liveops: list cloud saves: %w", err)
	}
	for _, slot := range local {
		seen[slot] = struct{}{}
	}
	for _, slot := range cloud {
		seen[slot] = struct{}{}
	}
	for _, slot := range s.stateSlots() {
		seen[slot] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for slot := range seen {
		out = append(out, slot)
	}
	slices.Sort(out)
	return out, nil
}

// SyncSlot reconciles one slot. On error nothing has been written for the slot, except that a failure after a conflict fork was created leaves the fork in place.
func (s *CloudSyncer) SyncSlot(ctx context.Context, slot string) (SlotResult, error) {
	res := SlotResult{Slot: slot}
	if err := save.ValidateSlot(slot); err != nil {
		return res, fmt.Errorf("liveops: sync: %w", err)
	}
	st, err := s.loadState(slot)
	if err != nil {
		return res, err
	}
	local, err := s.readLocal(slot)
	if err != nil {
		return res, err
	}
	cloud, err := s.readCloud(ctx, slot)
	if err != nil {
		return res, err
	}

	lh, ch := local.Hash, cloud.Hash
	switch {
	case lh == ch:
		if lh == "" {
			s.removeState(slot)
			return res, nil
		}
		st.Revision, st.Hash = cloud.Revision, ch
		return res, s.storeState(slot, st)
	case lh == st.Hash:
		res.Outcome, err = s.applyCloud(slot, cloud)
		return res, err
	case ch == st.Hash:
		res.Outcome, err = s.pushLocal(ctx, slot, local, cloud)
		return res, err
	}

	local.Revision = st.Revision
	if t, err := parseTime(st.LocalModified); err == nil && local.Exists {
		local.Modified = t
	}
	if local.Exists {
		local.Device = s.device
	}
	if s.summarize != nil {
		if local.Exists {
			local.Summary = s.summarize(slot, local.Data)
		}
		if cloud.Exists {
			cloud.Summary = s.summarize(slot, cloud.Data)
		}
	}
	c := Conflict{Slot: slot, Local: local, Cloud: cloud}
	res.Conflict = &c

	choice, err := s.decide(ctx, c)
	if err != nil {
		return res, fmt.Errorf("liveops: resolve conflict in %q: %w", slot, err)
	}
	if choice == ChoiceBoth && (!local.Exists || !cloud.Exists) {
		// Keeping both of a deleted and a changed copy means keeping the changed one.
		if local.Exists {
			choice = ChoiceLocal
		} else {
			choice = ChoiceCloud
		}
	}
	switch choice {
	case ChoiceDefer:
		res.Outcome = OutcomeDeferred
		return res, nil
	case ChoiceLocal:
		if _, err := s.pushLocal(ctx, slot, local, cloud); err != nil {
			return res, err
		}
		discarded := cloud
		res.Outcome, res.Discarded = OutcomeKeptLocal, &discarded
		return res, nil
	case ChoiceCloud:
		if _, err := s.applyCloud(slot, cloud); err != nil {
			return res, err
		}
		discarded := local
		res.Outcome, res.Discarded = OutcomeKeptCloud, &discarded
		return res, nil
	case ChoiceBoth:
		fork, err := s.fork(ctx, slot, local, cloud)
		if err != nil {
			return res, err
		}
		res.Outcome, res.Fork = OutcomeKeptBoth, fork
		return res, nil
	default:
		return res, fmt.Errorf("liveops: resolve conflict in %q: unknown choice %d", slot, int(choice))
	}
}

func (s *CloudSyncer) decide(ctx context.Context, c Conflict) (Choice, error) {
	switch s.policy {
	case PolicyAsk:
		return s.ask(ctx, c)
	case PolicyNewest:
		switch {
		case !c.Local.Exists:
			return ChoiceCloud, nil
		case !c.Cloud.Exists:
			return ChoiceLocal, nil
		case c.Local.Modified.IsZero() || c.Cloud.Modified.IsZero() || c.Local.Modified.Equal(c.Cloud.Modified):
			return ChoiceBoth, nil
		case c.Local.Modified.After(c.Cloud.Modified):
			return ChoiceLocal, nil
		default:
			return ChoiceCloud, nil
		}
	default:
		return ChoiceBoth, nil
	}
}

// readLocal returns the local copy, falling back to the backup when the primary is missing, as save.Store.Load does.
func (s *CloudSyncer) readLocal(slot string) (SaveVersion, error) {
	data, err := s.local.Read(slot)
	if errors.Is(err, save.ErrNotFound) {
		data, err = s.local.ReadBackup(slot)
	}
	if errors.Is(err, save.ErrNotFound) {
		return SaveVersion{}, nil
	}
	if err != nil {
		return SaveVersion{}, fmt.Errorf("liveops: read local save %q: %w", slot, err)
	}
	return SaveVersion{Exists: true, Hash: hashData(data), Data: data}, nil
}

func (s *CloudSyncer) readCloud(ctx context.Context, slot string) (SaveVersion, error) {
	raw, err := s.cloud.Load(ctx, slot)
	if errors.Is(err, ErrSlotNotFound) {
		return SaveVersion{}, nil
	}
	if err != nil {
		return SaveVersion{}, fmt.Errorf("liveops: load cloud save %q: %w", slot, err)
	}
	v, err := DecodeCloudSave(raw)
	if err != nil {
		return SaveVersion{}, fmt.Errorf("liveops: load cloud save %q: %w", slot, err)
	}
	return v, nil
}

// applyCloud makes the local copy match cloud and records cloud as the new base.
func (s *CloudSyncer) applyCloud(slot string, cloud SaveVersion) (SyncOutcome, error) {
	if !cloud.Exists {
		if err := s.local.Delete(slot); err != nil {
			return 0, fmt.Errorf("liveops: delete local save %q: %w", slot, err)
		}
		s.removeState(slot)
		return OutcomeDeletedLocal, nil
	}
	if err := s.local.Write(slot, cloud.Data); err != nil {
		return 0, fmt.Errorf("liveops: write local save %q: %w", slot, err)
	}
	st := syncState{Revision: cloud.Revision, Hash: cloud.Hash}
	if !cloud.Modified.IsZero() {
		st.LocalModified = formatTime(cloud.Modified)
	}
	return OutcomeDownloaded, s.storeState(slot, st)
}

// pushLocal makes the cloud copy match local, after checking that the cloud still holds prev, and records local as the new base.
func (s *CloudSyncer) pushLocal(ctx context.Context, slot string, local, prev SaveVersion) (SyncOutcome, error) {
	cur, err := s.readCloud(ctx, slot)
	if err != nil {
		return 0, err
	}
	if cur.Hash != prev.Hash || cur.Revision != prev.Revision {
		return 0, fmt.Errorf("liveops: upload %q: %w", slot, ErrCloudChanged)
	}
	if !local.Exists {
		if err := s.cloud.Delete(ctx, slot); err != nil {
			return 0, fmt.Errorf("liveops: delete cloud save %q: %w", slot, err)
		}
		s.removeState(slot)
		return OutcomeDeletedCloud, nil
	}
	st, err := s.loadState(slot)
	if err != nil {
		return 0, err
	}
	modified, err := parseTime(st.LocalModified)
	if err != nil || modified.IsZero() {
		modified = s.now()
	}
	next := SaveVersion{Exists: true, Revision: prev.Revision + 1, Hash: local.Hash, Modified: modified, Device: s.device, Data: local.Data}
	if err := s.upload(ctx, slot, next); err != nil {
		return 0, err
	}
	return OutcomeUploaded, s.storeState(slot, syncState{Revision: next.Revision, Hash: next.Hash, LocalModified: formatTime(modified)})
}

func (s *CloudSyncer) upload(ctx context.Context, slot string, v SaveVersion) error {
	payload, err := EncodeCloudSave(v)
	if err != nil {
		return fmt.Errorf("liveops: encode cloud save %q: %w", slot, err)
	}
	if err := s.cloud.Save(ctx, slot, payload); err != nil {
		return fmt.Errorf("liveops: save cloud save %q: %w", slot, err)
	}
	return nil
}

// fork moves the local copy into a new slot on both sides, then replaces the local slot with the cloud copy. The local fork is written first, so the local copy is never only in memory.
func (s *CloudSyncer) fork(ctx context.Context, slot string, local, cloud SaveVersion) (string, error) {
	name, err := s.forkName(ctx, slot)
	if err != nil {
		return "", err
	}
	if err := s.local.Write(name, local.Data); err != nil {
		return "", fmt.Errorf("liveops: write conflict copy %q: %w", name, err)
	}
	modified := local.Modified
	if modified.IsZero() {
		modified = s.now()
	}
	forked := SaveVersion{Exists: true, Revision: 1, Hash: local.Hash, Modified: modified, Device: s.device, Data: local.Data}
	if err := s.upload(ctx, name, forked); err != nil {
		return name, err
	}
	if err := s.storeState(name, syncState{Revision: 1, Hash: local.Hash, LocalModified: formatTime(modified)}); err != nil {
		return name, err
	}
	if _, err := s.applyCloud(slot, cloud); err != nil {
		return name, err
	}
	return name, nil
}

// forkName returns "<slot>.conflict-<device>", or with "-2", "-3" and so on appended, choosing the first name no side uses.
func (s *CloudSyncer) forkName(ctx context.Context, slot string) (string, error) {
	cloud, err := s.cloud.List(ctx)
	if err != nil {
		return "", fmt.Errorf("liveops: list cloud saves: %w", err)
	}
	base := slot + ".conflict-" + s.device
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name += "-" + strconv.Itoa(n)
		}
		if err := save.ValidateSlot(name); err != nil {
			return "", fmt.Errorf("liveops: conflict copy of %q: %w", slot, err)
		}
		if s.local.Exists(name) || slices.Contains(cloud, name) {
			continue
		}
		if _, ok := s.state.GetItem(s.stateKey(name)); ok {
			continue
		}
		return name, nil
	}
}

// ---- cloud payload ----

type cloudEnvelope struct {
	Format     string  `json:"format"`
	Version    int     `json:"version"`
	Revision   int64   `json:"revision"`
	Hash       string  `json:"hash"`
	Modified   string  `json:"modified,omitempty"`
	Device     string  `json:"device,omitempty"`
	Data       *string `json:"data,omitempty"`
	DataBase64 *string `json:"data_base64,omitempty"`
}

// EncodeCloudSave returns the JSON text CloudSyncer stores in the cloud for v: format, version, revision, hash, modified time (RFC 3339, UTC), device and the save data, in that fixed order. Data that is valid UTF-8, such as a save.Store payload, is kept as a readable string; other data is base64 in data_base64. v.Hash is recomputed from v.Data and v.Summary is not stored.
func EncodeCloudSave(v SaveVersion) ([]byte, error) {
	if v.Revision < 1 {
		return nil, fmt.Errorf("liveops: cloud save revision %d must be at least 1", v.Revision)
	}
	if v.Device != "" {
		if err := validateDevice(v.Device); err != nil {
			return nil, err
		}
	}
	env := cloudEnvelope{
		Format:   CloudSaveFormat,
		Version:  CloudSaveVersion,
		Revision: v.Revision,
		Hash:     hashData(v.Data),
		Device:   v.Device,
	}
	if !v.Modified.IsZero() {
		env.Modified = formatTime(v.Modified)
	}
	if utf8.Valid(v.Data) {
		s := string(v.Data)
		env.Data = &s
	} else {
		s := base64.StdEncoding.EncodeToString(v.Data)
		env.DataBase64 = &s
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeCloudSave reads a cloud payload. A JSON object whose "format" is CloudSaveFormat is decoded strictly (unknown fields, a newer version, a hash that does not match the data or a malformed value return an error wrapping ErrCorruptCloudSave); any other payload is a plain save written without CloudSyncer and is returned as revision 0 with no modified time or device.
func DecodeCloudSave(raw []byte) (SaveVersion, error) {
	if !isEnvelope(raw) {
		return SaveVersion{Exists: true, Hash: hashData(raw), Data: raw}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var env cloudEnvelope
	if err := dec.Decode(&env); err != nil {
		return SaveVersion{}, fmt.Errorf("%w: %v", ErrCorruptCloudSave, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return SaveVersion{}, fmt.Errorf("%w: trailing data", ErrCorruptCloudSave)
	}
	if env.Version < 1 || env.Version > CloudSaveVersion {
		return SaveVersion{}, fmt.Errorf("%w: unsupported version %d", ErrCorruptCloudSave, env.Version)
	}
	if env.Revision < 1 {
		return SaveVersion{}, fmt.Errorf("%w: revision %d", ErrCorruptCloudSave, env.Revision)
	}
	if env.Device != "" && validateDevice(env.Device) != nil {
		return SaveVersion{}, fmt.Errorf("%w: device %q", ErrCorruptCloudSave, env.Device)
	}
	var data []byte
	switch {
	case env.Data != nil && env.DataBase64 == nil:
		data = []byte(*env.Data)
	case env.Data == nil && env.DataBase64 != nil:
		d, err := base64.StdEncoding.Strict().DecodeString(*env.DataBase64)
		if err != nil {
			return SaveVersion{}, fmt.Errorf("%w: data_base64: %v", ErrCorruptCloudSave, err)
		}
		data = d
	default:
		return SaveVersion{}, fmt.Errorf("%w: needs exactly one of data and data_base64", ErrCorruptCloudSave)
	}
	if h := hashData(data); h != env.Hash {
		return SaveVersion{}, fmt.Errorf("%w: hash %q does not match data (%s)", ErrCorruptCloudSave, env.Hash, h)
	}
	v := SaveVersion{Exists: true, Revision: env.Revision, Hash: env.Hash, Device: env.Device, Data: data}
	if env.Modified != "" {
		t, err := parseTime(env.Modified)
		if err != nil {
			return SaveVersion{}, fmt.Errorf("%w: modified: %v", ErrCorruptCloudSave, err)
		}
		v.Modified = t
	}
	return v, nil
}

// isEnvelope reports whether raw starts with a JSON object whose top-level "format" is CloudSaveFormat.
func isEnvelope(raw []byte) bool {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || t[0] != '{' {
		return false
	}
	// Only the first JSON value decides, so an envelope with trailing data is still decoded strictly and refused.
	var probe struct {
		Format json.RawMessage `json:"format"`
	}
	if json.NewDecoder(bytes.NewReader(t)).Decode(&probe) != nil {
		return false
	}
	var format string
	return json.Unmarshal(probe.Format, &format) == nil && format == CloudSaveFormat
}

func hashData(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// ---- sync state ----

// syncState is what this device remembers about a slot, kept as JSON text in core.Storage.
type syncState struct {
	Version int `json:"version"`
	// Revision is the cloud revision at the last sync.
	Revision int64 `json:"revision"`
	// Hash is the content hash both sides held after the last sync; empty when the slot has never synced.
	Hash string `json:"hash,omitempty"`
	// LocalModified is when the local copy was last written, RFC 3339 UTC.
	LocalModified string `json:"local_modified,omitempty"`
}

func (s *CloudSyncer) stateKey(slot string) string {
	return s.prefix + "." + strings.NewReplacer("%", "%25", ".", "%2E").Replace(slot)
}

func (s *CloudSyncer) stateSlots() []string {
	pref := s.prefix + "."
	unescape := strings.NewReplacer("%2E", ".", "%25", "%")
	var out []string
	for _, k := range s.state.Keys() {
		if rest, ok := strings.CutPrefix(k, pref); ok && !strings.Contains(rest, ".") {
			out = append(out, unescape.Replace(rest))
		}
	}
	return out
}

func (s *CloudSyncer) loadState(slot string) (syncState, error) {
	raw, ok := s.state.GetItem(s.stateKey(slot))
	if !ok {
		return syncState{}, nil
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var st syncState
	if err := dec.Decode(&st); err != nil {
		return syncState{}, fmt.Errorf("%w: %q: %v", ErrCorruptSyncState, slot, err)
	}
	if st.Version != syncStateVersion || st.Revision < 0 {
		return syncState{}, fmt.Errorf("%w: %q: version %d, revision %d", ErrCorruptSyncState, slot, st.Version, st.Revision)
	}
	if _, err := parseTime(st.LocalModified); err != nil {
		return syncState{}, fmt.Errorf("%w: %q: %v", ErrCorruptSyncState, slot, err)
	}
	return st, nil
}

func (s *CloudSyncer) storeState(slot string, st syncState) error {
	st.Version = syncStateVersion
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := s.state.SetItem(s.stateKey(slot), string(raw)); err != nil {
		return fmt.Errorf("liveops: store sync state for %q: %w", slot, err)
	}
	return nil
}

func (s *CloudSyncer) removeState(slot string) { s.state.RemoveItem(s.stateKey(slot)) }
