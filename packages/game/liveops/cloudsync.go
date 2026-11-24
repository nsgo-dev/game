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
	"math"
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
	// ErrConflictChanged reports that a slot passed to CloudSyncer.Resolve no longer holds the copies of the Conflict it was given, because a save or a sync changed either side since. Nothing was written; sync again to see the current conflict, if any.
	ErrConflictChanged = errors.New("liveops: save changed since the conflict was reported")
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
	// ChoiceCloud keeps the cloud copy and overwrites (or deletes) this device's copy. SlotResult.Discarded holds the replaced copy; when it was a valid save envelope and the cloud copy exists, the local save backend also keeps it as its backup.
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
	// OutcomeKeptCloud means a conflict was resolved in favour of the cloud copy; SlotResult.Discarded holds the local copy it replaced. When that copy was a valid save envelope and the cloud copy exists, the local save backend also keeps it as its backup; otherwise Discarded is the only copy left.
	OutcomeKeptCloud
	// OutcomeKeptBoth means a conflict was resolved by keeping the cloud copy in the slot and moving the local copy to SlotResult.Fork on both sides.
	OutcomeKeptBoth
	// OutcomeDeferred means a conflict was left unresolved, by ChoiceDefer under PolicyAsk or passed to CloudSyncer.Resolve; nothing was written.
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
	// Modified is when the copy was last written, zero when unknown (a local write that bypassed CloudSyncer.Backend, or a plain cloud payload). A local time counts only for the exact copy it was recorded with: a later write that bypassed Backend makes it unknown.
	Modified time.Time
	// Device is the ID of the device that wrote the copy, empty when unknown.
	Device string
	// Summary is CloudSyncOptions.Summarize's text for Data, such as "Level 12, 3h 40m played".
	Summary string
	// Data is the raw save payload.
	Data []byte
}

// Conflict is a slot whose local and cloud copies both changed since this device last synced it, passed to CloudSyncOptions.Ask under PolicyAsk and reported in SlotResult.Conflict. Pass it back to CloudSyncer.Resolve to apply the player's choice later.
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
	// Ask answers each conflict under PolicyAsk. It is required with PolicyAsk. It runs inside the sync, on the app's thread, so it must return without waiting for the player: to ask through a dialog, keep the Conflict, return ChoiceDefer, show both summaries, and apply the answer with CloudSyncer.Resolve. An error leaves the slot untouched and is returned by the sync.
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
// Write saves through Backend so that each local write is stamped with its time for PolicyNewest and conflict summaries. A CloudSyncer is not safe for concurrent use; call Sync, SyncSlot and Resolve where the app's code runs, as core.Storage requires, or use SyncAsync, SyncSlotAsync and ResolveAsync to run cloud I/O off the engine goroutine while marshaling local storage, sync state and Ask callbacks onto cx's engine goroutine (required with PlatformCloud on Apple and Android devices, and recommended with CloudClient so network I/O does not stall frames).
//
// Before every cloud write it reads the cloud copy again and refuses to write when it changed (ErrCloudChanged). When Cloud implements ConditionalCloudSync, as CloudClient and DirCloud do, the write itself is conditional on that copy, so an upload by another device can never be overwritten: the losing write fails with ErrCloudChanged and the next sync sees both copies as a conflict for the policy to resolve. A plain CloudSync has no conditional write, so an upload by another device that lands between the re-read and the write can still be overwritten; the window is a single Load and Save apart.
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
	asyncSem  chan struct{}
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
		asyncSem:  make(chan struct{}, 1),
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

// Backend returns the local save backend wrapped so that every Write and Delete is stamped with the current time in the slot's sync state. Pass it to save.NewStore so PolicyNewest and conflict summaries know when each local copy was written. Its Unwrap method returns the local backend, so a save.SettingsStore built on it keeps settings where the local backend does.
func (s *CloudSyncer) Backend() save.Backend { return trackedBackend{s} }

type trackedBackend struct{ s *CloudSyncer }

// Unwrap returns the local backend, so save sees through the wrapper (settings stores keep their own key and are never synced).
func (b trackedBackend) Unwrap() save.Backend { return b.s.local }

func (b trackedBackend) Read(slot string) ([]byte, error)       { return b.s.local.Read(slot) }
func (b trackedBackend) ReadBackup(slot string) ([]byte, error) { return b.s.local.ReadBackup(slot) }
func (b trackedBackend) Exists(slot string) bool                { return b.s.local.Exists(slot) }
func (b trackedBackend) Slots() ([]string, error)               { return b.s.local.Slots() }

func (b trackedBackend) Write(slot string, data []byte) error {
	if err := b.s.local.Write(slot, data); err != nil {
		return err
	}
	return b.s.stampLocal(slot, hashData(data))
}

func (b trackedBackend) Delete(slot string) error {
	if err := b.s.local.Delete(slot); err != nil {
		return err
	}
	return b.s.stampLocal(slot, "")
}

// stampLocal records that the local copy of slot, with content hash hash ("" after a delete), was written now. Names that are not save slots, such as "settings" written by a settings store on a backend without Unwrap, are not synced and get no state.
func (s *CloudSyncer) stampLocal(slot, hash string) error {
	if save.ValidateSlot(slot) != nil {
		return nil
	}
	st, err := s.loadState(slot)
	if err != nil {
		// A corrupt record must not block saving; start it over with only the stamp, which turns the next sync of this slot into a first sync.
		st = syncState{}
	}
	st.LocalModified, st.LocalHash = formatTime(s.now()), hash
	return s.storeState(slot, st)
}

// Sync reconciles every slot known locally, in the cloud or in the sync state, in slot-name order.
//
// A slot that fails is skipped and its error joined into the returned error while the others still sync, except when the cloud is unavailable or ctx is done, which stops the sync.
func (s *CloudSyncer) Sync(ctx context.Context) (SyncResult, error) {
	return syncOp{s: s}.sync(ctx)
}

// SyncSlot reconciles one slot. On error nothing has been written for the slot, except that a failure after a conflict fork was created leaves the fork in place; the next sync uploads it and reuses it rather than creating another.
func (s *CloudSyncer) SyncSlot(ctx context.Context, slot string) (SlotResult, error) {
	return syncOp{s: s}.syncSlot(ctx, slot)
}

// Resolve applies choice to a conflict reported earlier, typically one that Ask answered with ChoiceDefer so the game could show the player both copies outside the sync. It reads both sides again and applies choice only while they still hold the copies in c (the same existence, content hash and cloud revision); otherwise it writes nothing and returns ErrConflictChanged. ChoiceDefer writes nothing. Resolve works under every policy and does not call Ask.
func (s *CloudSyncer) Resolve(ctx context.Context, c Conflict, choice Choice) (SlotResult, error) {
	return syncOp{s: s}.resolve(ctx, c, choice)
}

// SyncAsync runs Sync with cloud I/O off cx's engine goroutine while marshaling local backend, sync state, Summarize, Now and Ask calls onto cx's engine goroutine through cx.Post, then delivers the result to done on the engine goroutine. Use it on Apple and Android devices with PlatformCloud (whose callbacks arrive via cx.Post) and with CloudClient so network I/O does not block frames.
//
// Concurrent SyncAsync, SyncSlotAsync and ResolveAsync calls on the same CloudSyncer are serialized in order. If a local save changes a slot while its cloud read is in flight, SyncAsync refuses to overwrite the local copy (ErrConflictChanged) so the next sync resolves both copies cleanly. Disposing cx stops the sync and skips done.
func (s *CloudSyncer) SyncAsync(cx *core.Scope, ctx context.Context, done func(SyncResult, error)) {
	if cx == nil {
		if done != nil {
			done(SyncResult{}, errors.New("liveops: sync async needs a non-nil scope"))
		}
		return
	}
	if cx.Disposed() {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	scopeCtx := cx.Context()
	go func() {
		if err := s.acquireAsync(ctx, scopeCtx); err != nil {
			if scopeCtx.Err() == nil && done != nil {
				cx.Post(func() { done(SyncResult{}, err) })
			}
			return
		}
		defer s.releaseAsync()
		op := syncOp{s: s, app: scopeRunner(cx, scopeCtx, ctx)}
		res, err := op.sync(ctx)
		if scopeCtx.Err() == nil && done != nil {
			cx.Post(func() { done(res, err) })
		}
	}()
}

// SyncSlotAsync runs SyncSlot asynchronously as SyncAsync does for Sync.
func (s *CloudSyncer) SyncSlotAsync(cx *core.Scope, ctx context.Context, slot string, done func(SlotResult, error)) {
	if cx == nil {
		if done != nil {
			done(SlotResult{Slot: slot}, errors.New("liveops: sync slot async needs a non-nil scope"))
		}
		return
	}
	if cx.Disposed() {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	scopeCtx := cx.Context()
	go func() {
		if err := s.acquireAsync(ctx, scopeCtx); err != nil {
			if scopeCtx.Err() == nil && done != nil {
				cx.Post(func() { done(SlotResult{Slot: slot}, err) })
			}
			return
		}
		defer s.releaseAsync()
		op := syncOp{s: s, app: scopeRunner(cx, scopeCtx, ctx)}
		res, err := op.syncSlot(ctx, slot)
		if scopeCtx.Err() == nil && done != nil {
			cx.Post(func() { done(res, err) })
		}
	}()
}

// ResolveAsync runs Resolve asynchronously as SyncAsync does for Sync.
func (s *CloudSyncer) ResolveAsync(cx *core.Scope, ctx context.Context, c Conflict, choice Choice, done func(SlotResult, error)) {
	if cx == nil {
		if done != nil {
			done(SlotResult{Slot: c.Slot}, errors.New("liveops: resolve async needs a non-nil scope"))
		}
		return
	}
	if cx.Disposed() {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	scopeCtx := cx.Context()
	go func() {
		if err := s.acquireAsync(ctx, scopeCtx); err != nil {
			if scopeCtx.Err() == nil && done != nil {
				cx.Post(func() { done(SlotResult{Slot: c.Slot}, err) })
			}
			return
		}
		defer s.releaseAsync()
		op := syncOp{s: s, app: scopeRunner(cx, scopeCtx, ctx)}
		res, err := op.resolve(ctx, c, choice)
		if scopeCtx.Err() == nil && done != nil {
			cx.Post(func() { done(res, err) })
		}
	}()
}

func (s *CloudSyncer) acquireAsync(ctx, scopeCtx context.Context) error {
	select {
	case s.asyncSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-scopeCtx.Done():
		return ErrCloudUnavailable
	}
}

func (s *CloudSyncer) releaseAsync() { <-s.asyncSem }

func scopeRunner(cx *core.Scope, scopeCtx, ctx context.Context) func(func()) error {
	return func(fn func()) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if scopeCtx.Err() != nil || cx.Disposed() {
			return ErrCloudUnavailable
		}
		var cancelled bool
		ch := make(chan struct{})
		cx.Post(func() {
			defer close(ch)
			if ctx.Err() != nil {
				cancelled = true
				return
			}
			fn()
		})
		select {
		case <-ch:
			if cancelled {
				return ctx.Err()
			}
			return nil
		case <-scopeCtx.Done():
			return ErrCloudUnavailable
		}
	}
}

// syncOp executes one Sync, SyncSlot or Resolve pass. When app is nil (synchronous calls), local backend, sync state and callback reads run inline on the caller's goroutine; when app is non-nil (SyncAsync), they run on cx's engine goroutine while cloud I/O runs on the worker goroutine.
type syncOp struct {
	s   *CloudSyncer
	app func(func()) error
}

func (op syncOp) onApp(fn func()) error {
	if op.app == nil {
		fn()
		return nil
	}
	return op.app(fn)
}

func (op syncOp) sync(ctx context.Context) (SyncResult, error) {
	var res SyncResult
	slots, err := op.allSlots(ctx)
	if err != nil {
		return res, err
	}
	var errs []error
	for _, slot := range slots {
		if err := ctx.Err(); err != nil {
			return res, errors.Join(append(errs, err)...)
		}
		r, err := op.syncSlot(ctx, slot)
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

func (op syncOp) allSlots(ctx context.Context) ([]string, error) {
	s := op.s
	var local, state []string
	var localErr error
	if err := op.onApp(func() {
		local, localErr = s.local.Slots()
		if localErr == nil {
			state = s.stateSlots()
		}
	}); err != nil {
		return nil, err
	}
	if localErr != nil {
		return nil, fmt.Errorf("liveops: list local saves: %w", localErr)
	}
	cloud, err := s.cloud.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("liveops: list cloud saves: %w", err)
	}
	seen := make(map[string]struct{}, len(local)+len(cloud)+len(state))
	for _, slot := range local {
		seen[slot] = struct{}{}
	}
	for _, slot := range cloud {
		seen[slot] = struct{}{}
	}
	for _, slot := range state {
		seen[slot] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for slot := range seen {
		out = append(out, slot)
	}
	slices.Sort(out)
	return out, nil
}

func (op syncOp) syncSlot(ctx context.Context, slot string) (SlotResult, error) {
	res := SlotResult{Slot: slot}
	st, local, cloud, err := op.readSides(ctx, slot)
	if err != nil {
		return res, err
	}

	lh, ch := local.Hash, cloud.Hash
	switch {
	case lh == ch:
		var writeErr error
		if err := op.onApp(func() {
			if op.app != nil {
				cur, rerr := op.s.readLocal(slot)
				if rerr != nil {
					writeErr = rerr
					return
				}
				if cur.Hash != lh {
					writeErr = fmt.Errorf("liveops: sync %q: %w", slot, ErrConflictChanged)
					return
				}
			}
			if lh == "" {
				op.s.removeState(slot)
				return
			}
			st.Revision, st.Hash = cloud.Revision, ch
			writeErr = op.s.storeState(slot, st)
		}); err != nil {
			return res, err
		}
		return res, writeErr
	case lh == st.Hash:
		res.Outcome, err = op.applyCloud(slot, cloud, lh)
		return res, err
	case ch == st.Hash:
		res.Outcome, err = op.pushLocal(ctx, slot, local, cloud)
		return res, err
	}

	c, err := op.conflict(slot, st, local, cloud)
	if err != nil {
		return res, err
	}
	res.Conflict = &c
	choice, err := op.decide(ctx, c)
	if err != nil {
		return res, fmt.Errorf("liveops: resolve conflict in %q: %w", slot, err)
	}
	return op.apply(ctx, res, choice)
}

func (op syncOp) resolve(ctx context.Context, c Conflict, choice Choice) (SlotResult, error) {
	res := SlotResult{Slot: c.Slot}
	if choice < ChoiceDefer || choice > ChoiceBoth {
		return res, fmt.Errorf("liveops: resolve conflict in %q: unknown choice %d", c.Slot, int(choice))
	}
	st, local, cloud, err := op.readSides(ctx, c.Slot)
	if err != nil {
		return res, err
	}
	if local.Exists != c.Local.Exists || local.Hash != c.Local.Hash || cloud.Exists != c.Cloud.Exists || cloud.Hash != c.Cloud.Hash || cloud.Revision != c.Cloud.Revision || local.Hash == cloud.Hash {
		return res, fmt.Errorf("liveops: resolve conflict in %q: %w", c.Slot, ErrConflictChanged)
	}
	cur, err := op.conflict(c.Slot, st, local, cloud)
	if err != nil {
		return res, err
	}
	res.Conflict = &cur
	return op.apply(ctx, res, choice)
}

// readSides validates slot and reads its sync state, local copy and cloud copy.
func (op syncOp) readSides(ctx context.Context, slot string) (syncState, SaveVersion, SaveVersion, error) {
	if err := save.ValidateSlot(slot); err != nil {
		return syncState{}, SaveVersion{}, SaveVersion{}, fmt.Errorf("liveops: sync: %w", err)
	}
	var st syncState
	var local SaveVersion
	var appErr error
	if err := op.onApp(func() {
		st, appErr = op.s.loadState(slot)
		if appErr != nil {
			return
		}
		local, appErr = op.s.readLocal(slot)
	}); err != nil {
		return syncState{}, SaveVersion{}, SaveVersion{}, err
	}
	if appErr != nil {
		return syncState{}, SaveVersion{}, SaveVersion{}, appErr
	}
	cloud, err := op.s.readCloud(ctx, slot)
	if err != nil {
		return syncState{}, SaveVersion{}, SaveVersion{}, err
	}
	return st, local, cloud, nil
}

// conflict fills in the local side's sync metadata and both summaries.
func (op syncOp) conflict(slot string, st syncState, local, cloud SaveVersion) (Conflict, error) {
	s := op.s
	local.Revision = st.Revision
	if local.Exists {
		local.Modified = st.localTime(local.Hash)
		local.Device = s.device
	}
	if s.summarize != nil {
		if err := op.onApp(func() {
			if local.Exists {
				local.Summary = s.summarize(slot, local.Data)
			}
			if cloud.Exists {
				cloud.Summary = s.summarize(slot, cloud.Data)
			}
		}); err != nil {
			return Conflict{}, err
		}
	}
	return Conflict{Slot: slot, Local: local, Cloud: cloud}, nil
}

// apply carries out choice for the conflict in res.
func (op syncOp) apply(ctx context.Context, res SlotResult, choice Choice) (SlotResult, error) {
	c := *res.Conflict
	slot := c.Slot
	if choice == ChoiceBoth && (!c.Local.Exists || !c.Cloud.Exists) {
		// Keeping both of a deleted and a changed copy means keeping the changed one.
		if c.Local.Exists {
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
		if _, err := op.pushLocal(ctx, slot, c.Local, c.Cloud); err != nil {
			return res, err
		}
		discarded := c.Cloud
		res.Outcome, res.Discarded = OutcomeKeptLocal, &discarded
		return res, nil
	case ChoiceCloud:
		if _, err := op.applyCloud(slot, c.Cloud, c.Local.Hash); err != nil {
			return res, err
		}
		discarded := c.Local
		res.Outcome, res.Discarded = OutcomeKeptCloud, &discarded
		return res, nil
	case ChoiceBoth:
		fork, err := op.fork(ctx, slot, c.Local, c.Cloud)
		res.Fork = fork
		if err != nil {
			return res, err
		}
		res.Outcome = OutcomeKeptBoth
		return res, nil
	default:
		return res, fmt.Errorf("liveops: resolve conflict in %q: unknown choice %d", slot, int(choice))
	}
}

func (op syncOp) decide(ctx context.Context, c Conflict) (Choice, error) {
	s := op.s
	switch s.policy {
	case PolicyAsk:
		var choice Choice
		var askErr error
		if err := op.onApp(func() {
			choice, askErr = s.ask(ctx, c)
		}); err != nil {
			return ChoiceDefer, err
		}
		return choice, askErr
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
	v, _, err := s.readCloudTagged(ctx, slot)
	return v, err
}

// readCloudTagged reads the cloud copy of slot and the PayloadHash of its raw payload, "" when the slot does not exist.
func (s *CloudSyncer) readCloudTagged(ctx context.Context, slot string) (SaveVersion, string, error) {
	raw, err := s.cloud.Load(ctx, slot)
	if errors.Is(err, ErrSlotNotFound) {
		return SaveVersion{}, "", nil
	}
	if err != nil {
		return SaveVersion{}, "", fmt.Errorf("liveops: load cloud save %q: %w", slot, err)
	}
	v, err := DecodeCloudSave(raw)
	if err != nil {
		return SaveVersion{}, "", fmt.Errorf("liveops: load cloud save %q: %w", slot, err)
	}
	return v, PayloadHash(raw), nil
}

// applyCloud makes the local copy match cloud and records cloud as the new base. When running asynchronously (op.app != nil), it verifies the local copy still has expectedLocalHash before overwriting it so a save made while the cloud read was in flight is never clobbered.
func (op syncOp) applyCloud(slot string, cloud SaveVersion, expectedLocalHash string) (SyncOutcome, error) {
	s := op.s
	var outcome SyncOutcome
	var writeErr error
	if err := op.onApp(func() {
		if op.app != nil {
			cur, rerr := s.readLocal(slot)
			if rerr != nil {
				writeErr = rerr
				return
			}
			if cur.Hash != expectedLocalHash {
				writeErr = fmt.Errorf("liveops: apply cloud save %q: %w", slot, ErrConflictChanged)
				return
			}
		}
		if !cloud.Exists {
			if err := s.local.Delete(slot); err != nil {
				writeErr = fmt.Errorf("liveops: delete local save %q: %w", slot, err)
				return
			}
			s.removeState(slot)
			outcome = OutcomeDeletedLocal
			return
		}
		if err := s.local.Write(slot, cloud.Data); err != nil {
			writeErr = fmt.Errorf("liveops: write local save %q: %w", slot, err)
			return
		}
		st := syncState{Revision: cloud.Revision, Hash: cloud.Hash}
		if !cloud.Modified.IsZero() {
			st.LocalModified, st.LocalHash = formatTime(cloud.Modified), cloud.Hash
		}
		outcome = OutcomeDownloaded
		writeErr = s.storeState(slot, st)
	}); err != nil {
		return 0, err
	}
	return outcome, writeErr
}

// pushLocal makes the cloud copy match local, after checking that the cloud still holds prev, and records local as the new base.
func (op syncOp) pushLocal(ctx context.Context, slot string, local, prev SaveVersion) (SyncOutcome, error) {
	s := op.s
	cur, tag, err := s.readCloudTagged(ctx, slot)
	if err != nil {
		return 0, err
	}
	if cur.Hash != prev.Hash || cur.Revision != prev.Revision {
		return 0, fmt.Errorf("liveops: upload %q: %w", slot, ErrCloudChanged)
	}
	if !local.Exists {
		if err := s.deleteCloud(ctx, slot, tag); err != nil {
			return 0, fmt.Errorf("liveops: delete cloud save %q: %w", slot, err)
		}
		if err := op.onApp(func() {
			if op.app != nil {
				if curLocal, rerr := s.readLocal(slot); rerr == nil && curLocal.Exists {
					return
				}
			}
			s.removeState(slot)
		}); err != nil {
			return 0, err
		}
		return OutcomeDeletedCloud, nil
	}
	var modified time.Time
	var stateErr error
	if err := op.onApp(func() {
		st, lerr := s.loadState(slot)
		if lerr != nil {
			stateErr = lerr
			return
		}
		// A time recorded for another copy, such as the one before a write that bypassed Backend, does not describe this one.
		modified = st.localTime(local.Hash)
		if modified.IsZero() {
			modified = s.now()
		}
	}); err != nil {
		return 0, err
	}
	if stateErr != nil {
		return 0, stateErr
	}
	next := SaveVersion{Exists: true, Revision: prev.Revision + 1, Hash: local.Hash, Modified: modified, Device: s.device, Data: local.Data}
	if err := s.upload(ctx, slot, next, tag); err != nil {
		return 0, err
	}
	var storeErr error
	if err := op.onApp(func() {
		st := syncState{Revision: next.Revision, Hash: next.Hash, LocalModified: formatTime(modified), LocalHash: next.Hash}
		if op.app != nil {
			// If the player saved a newer local copy while the upload was in flight, keep its LocalModified/LocalHash so the next sync uploads it with its real timestamp.
			if prevSt, lerr := s.loadState(slot); lerr == nil && prevSt.LocalHash != local.Hash && prevSt.LocalModified != "" {
				st.LocalModified, st.LocalHash = prevSt.LocalModified, prevSt.LocalHash
			}
		}
		storeErr = s.storeState(slot, st)
	}); err != nil {
		return 0, err
	}
	return OutcomeUploaded, storeErr
}

// upload writes v to slot, conditional on the slot still holding the payload with PayloadHash match ("" for none) when the cloud supports it.
func (s *CloudSyncer) upload(ctx context.Context, slot string, v SaveVersion, match string) error {
	payload, err := EncodeCloudSave(v)
	if err != nil {
		return fmt.Errorf("liveops: encode cloud save %q: %w", slot, err)
	}
	if cs, ok := s.cloud.(ConditionalCloudSync); ok {
		err = cs.SaveIf(ctx, slot, payload, match)
	} else {
		err = s.cloud.Save(ctx, slot, payload)
	}
	if err != nil {
		return fmt.Errorf("liveops: save cloud save %q: %w", slot, err)
	}
	return nil
}

// deleteCloud removes slot, conditional on it still holding the payload with PayloadHash match when the cloud supports it.
func (s *CloudSyncer) deleteCloud(ctx context.Context, slot, match string) error {
	if cs, ok := s.cloud.(ConditionalCloudSync); ok {
		return cs.DeleteIf(ctx, slot, match)
	}
	return s.cloud.Delete(ctx, slot)
}

// fork moves the local copy into a fork slot on both sides, then replaces the local slot with the cloud copy. The local fork and its sync state are written first, so the local copy is never only in memory and a failed upload is retried by the next sync, which also finds and reuses this fork instead of creating another.
func (op syncOp) fork(ctx context.Context, slot string, local, cloud SaveVersion) (string, error) {
	s := op.s
	name, reuse, err := op.forkName(ctx, slot, local.Hash)
	if err != nil {
		return "", err
	}
	if !reuse {
		var forkWriteErr error
		var wroteLocal bool
		if err := op.onApp(func() {
			if err := s.local.Write(name, local.Data); err != nil {
				forkWriteErr = fmt.Errorf("liveops: write conflict copy %q: %w", name, err)
				return
			}
			wroteLocal = true
			modified := local.Modified
			if modified.IsZero() {
				modified = s.now()
			}
			// Never synced (no base hash), so the next sync of the fork uploads it if this one fails.
			forkWriteErr = s.storeState(name, syncState{LocalModified: formatTime(modified), LocalHash: local.Hash})
		}); err != nil {
			return "", err
		}
		if forkWriteErr != nil {
			if wroteLocal {
				return name, forkWriteErr
			}
			return "", forkWriteErr
		}
	}
	cur, err := s.readCloud(ctx, name)
	if err != nil {
		return name, err
	}
	forked := SaveVersion{Exists: true, Hash: local.Hash, Data: local.Data}
	if cur.Hash == local.Hash {
		var stateErr error
		if err := op.onApp(func() {
			st, lerr := s.loadState(name)
			if lerr != nil {
				stateErr = lerr
				return
			}
			st.Revision, st.Hash = cur.Revision, cur.Hash
			stateErr = s.storeState(name, st)
		}); err != nil {
			return name, err
		}
		if stateErr != nil {
			return name, stateErr
		}
	} else if _, err := op.pushLocal(ctx, name, forked, cur); err != nil {
		return name, err
	}
	if _, err := op.applyCloud(slot, cloud, local.Hash); err != nil {
		return name, err
	}
	return name, nil
}

// maxForkSlotLen is the longest slot name save.ValidateSlot accepts.
const maxForkSlotLen = 200

// forkName returns the fork slot for a conflict in slot whose local copy has content hash hash: "<slot>.conflict-<device>", or with "-2", "-3" and so on appended. It reuses (reuse true) a name that already holds this very copy locally and is absent from the cloud or holds the same copy there, left by an earlier fork that failed part way; otherwise it chooses the first name no side uses. When slot is too long for the name to fit, the slot part is shortened and followed by '~' and 8 hex digits of the slot's SHA-256, so forks of different long slots stay distinct.
func (op syncOp) forkName(ctx context.Context, slot, hash string) (string, bool, error) {
	s := op.s
	cloud, err := s.cloud.List(ctx)
	if err != nil {
		return "", false, fmt.Errorf("liveops: list cloud saves: %w", err)
	}
	for n := 1; ; n++ {
		suffix := ".conflict-" + s.device
		if n > 1 {
			suffix += "-" + strconv.Itoa(n)
		}
		name := forkSlotName(slot, suffix)
		if err := save.ValidateSlot(name); err != nil {
			return "", false, fmt.Errorf("liveops: conflict copy of %q: %w", slot, err)
		}
		inCloud := slices.Contains(cloud, name)
		var existsLocal, hasState bool
		var localVer SaveVersion
		var localErr error
		if err := op.onApp(func() {
			existsLocal = s.local.Exists(name)
			if existsLocal {
				localVer, localErr = s.readLocal(name)
				return
			}
			if !inCloud {
				_, hasState = s.state.GetItem(s.stateKey(name))
			}
		}); err != nil {
			return "", false, err
		}
		if localErr != nil {
			return "", false, localErr
		}
		if existsLocal {
			if localVer.Hash != hash {
				continue
			}
			if !inCloud {
				return name, true, nil
			}
			cv, err := s.readCloud(ctx, name)
			if err != nil {
				return "", false, err
			}
			if cv.Hash == hash {
				return name, true, nil
			}
			continue
		}
		if inCloud || hasState {
			continue
		}
		return name, false, nil
	}
}

// forkSlotName joins slot and suffix, shortening slot when the result would be longer than a slot name may be.
func forkSlotName(slot, suffix string) string {
	if len(slot)+len(suffix) <= maxForkSlotLen {
		return slot + suffix
	}
	sum := sha256.Sum256([]byte(slot))
	tag := "~" + hex.EncodeToString(sum[:4])
	keep := maxForkSlotLen - len(suffix) - len(tag)
	head := slot[:max(keep, 0)]
	// Do not split a multi-byte character.
	for len(head) > 0 {
		if r, size := utf8.DecodeLastRuneInString(head); r != utf8.RuneError || size > 1 {
			break
		}
		head = head[:len(head)-1]
	}
	// The cut must not leave a '.' that would join the tag into a "..", or a trailing space or dot.
	head = strings.TrimRight(head, ". ")
	return head + tag + suffix
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

// DecodeCloudSave reads a cloud payload. A JSON object whose "format" is CloudSaveFormat is decoded strictly (unknown fields, a newer version, a revision below 1 or at math.MaxInt64, a hash that does not match the data or a malformed value return an error wrapping ErrCorruptCloudSave); any other payload is a plain save written without CloudSyncer and is returned as revision 0 with no modified time or device.
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
	// The largest revision is refused too: the next upload could not increment it.
	if env.Revision < 1 || env.Revision == math.MaxInt64 {
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
	// LocalHash is the content hash of the local copy LocalModified was recorded for ("" for a deletion). A local copy with another hash was written without CloudSyncer.Backend, so its time is unknown.
	LocalHash string `json:"local_hash,omitempty"`
}

// localTime returns LocalModified when it was recorded for the local copy with content hash hash, and the zero time otherwise.
func (st syncState) localTime(hash string) time.Time {
	if st.LocalHash != hash {
		return time.Time{}
	}
	t, err := parseTime(st.LocalModified)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (s *CloudSyncer) stateKey(slot string) string {
	return s.prefix + "." + strings.NewReplacer("%", "%25", ".", "%2E").Replace(slot)
}

func (s *CloudSyncer) stateSlots() []string {
	pref := s.prefix + "."
	unescape := strings.NewReplacer("%2E", ".", "%25", "%")
	var out []string
	for _, k := range s.state.Keys() {
		rest, ok := strings.CutPrefix(k, pref)
		if !ok || strings.Contains(rest, ".") {
			continue
		}
		// A name that is not a save slot is not synced; skipping it keeps one stray key from failing every Sync.
		if slot := unescape.Replace(rest); save.ValidateSlot(slot) == nil {
			out = append(out, slot)
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
