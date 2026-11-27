package liveops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/cloud"
	"github.com/nsgo-dev/nsgo/core"
)

// DefaultCloudPrefix is the file-name prefix PlatformCloud prepends to each save slot when PlatformCloudOptions.Prefix is empty, storing slot "main" as "save.main.json". A flat dot-separated name without slashes works in both Apple iCloud ubiquity containers and Google Play Games Saved Games snapshots, and keeps save slots separate from other files in the container.
const DefaultCloudPrefix = "save."

// CloudProvider names a platform cloud-save service.
type CloudProvider uint8

const (
	// CloudProviderNone means the platform has no built-in OS cloud-save service through NSGo's cloud package (Windows, Linux, the web).
	CloudProviderNone CloudProvider = iota
	// CloudProviderICloud is Apple's iCloud ubiquity container on macOS and iOS (including iPadOS and tvOS).
	CloudProviderICloud
	// CloudProviderPlayGames is Google Play Games Saved Games on Android.
	CloudProviderPlayGames
	// CloudProviderSteam is Steam Auto-Cloud on desktop targets, which syncs a local directory (such as cx.DataDir() or a subdirectory via DirCloud).
	CloudProviderSteam
)

// String returns "none", "icloud", "play-games" or "steam".
func (p CloudProvider) String() string {
	switch p {
	case CloudProviderICloud:
		return "icloud"
	case CloudProviderPlayGames:
		return "play-games"
	case CloudProviderSteam:
		return "steam"
	default:
		return "none"
	}
}

// CloudProviderFor returns the built-in OS cloud-save service NSGo's cloud package reaches on os: CloudProviderICloud on macOS and iOS, CloudProviderPlayGames on Android, and CloudProviderNone elsewhere.
func CloudProviderFor(os core.OS) CloudProvider {
	switch os {
	case core.OSMacOS, core.OSIOS:
		return CloudProviderICloud
	case core.OSAndroid:
		return CloudProviderPlayGames
	default:
		return CloudProviderNone
	}
}

// PlatformCloudOptions configures NewPlatformCloud.
type PlatformCloudOptions struct {
	// Prefix is prepended to each slot name to form its cloud file or snapshot name ("<Prefix><slot>.json"); DefaultCloudPrefix ("save.") when empty. Must not start with '/' or '.', or contain "..", '\', "//" or control characters. Avoid '/' when targeting Android, where Google Play Games snapshot names do not accept path separators.
	Prefix string
}

// CloudChange reports save slots updated or deleted externally in platform cloud storage (such as from another device).
type CloudChange struct {
	// Slots lists the sorted save slot names affected by this change. It may be empty on account-change or initial-sync notifications that did not enumerate individual files.
	Slots []string
	// Reason explains why the external change arrived.
	Reason cloud.ChangeReason
}

// PlatformConflict describes a version conflict reported by the native platform cloud backend (iCloud NSFileVersion or Google Play Games Saved Games) for one save slot.
type PlatformConflict struct {
	// Slot is the save slot name.
	Slot string
	// Local holds the local cloud payload bytes (which may be a CloudSyncer JSON envelope decodable with DecodeCloudSave).
	Local []byte
	// Remote holds the conflicting remote cloud payload bytes.
	Remote []byte
	// LocalTime is the modification time reported for Local, if known.
	LocalTime time.Time
	// RemoteTime is the modification time reported for Remote, if known.
	RemoteTime time.Time
	// Resolve commits the winning payload bytes to platform cloud storage, clearing the native conflict.
	Resolve func(chosen []byte) error
}

// PlatformCloud implements CloudSync over NSGo's package cloud: Apple iCloud ubiquity containers on macOS and iOS, Google Play Games Saved Games on Android, and an isolated per-app in-memory store in nsgotest. On platforms without a built-in OS cloud store (Windows, Linux, the web), every operation fails with ErrCloudUnavailable and never silently drops writes.
//
// Each save slot is stored as "<Prefix><slot>.json" (default "save.<slot>.json") and validated with save.ValidateSlot. Errors wrap liveops's sentinels alongside package cloud's sentinels: ErrCloudUnavailable (cloud.ErrUnavailable), ErrSlotNotFound (cloud.ErrNotFound), and ErrCloudConflict (cloud.ErrConflict / *cloud.ConflictError).
//
// On real Apple and Android targets (where cx.CloudStore() is nil), package cloud delivers its callbacks on cx's engine goroutine through cx.Post, so blocking CloudSync methods (Load, Save, Delete, List) must be called off the engine goroutine—such as through CloudSyncer.SyncAsync—or they would block the goroutine needed to deliver their answer. Under nsgotest (where cx.CloudStore() is non-nil), operations run directly against the per-app store and are safe from any goroutine, including synchronous CloudSyncer.Sync calls in tests.
type PlatformCloud struct {
	cx       *core.Scope
	scopeCtx context.Context
	provider CloudProvider
	prefix   string
}

var _ CloudSync = (*PlatformCloud)(nil)

// NewPlatformCloud returns a PlatformCloud bound to cx, or an error if cx is nil or opts.Prefix is invalid. Call it on cx's engine goroutine with a scope that lives as long as cloud sync runs (such as the app's root scope).
func NewPlatformCloud(cx *core.Scope, opts PlatformCloudOptions) (*PlatformCloud, error) {
	if cx == nil {
		return nil, errors.New("liveops: platform cloud needs a non-nil scope")
	}
	prefix := opts.Prefix
	if prefix == "" {
		prefix = DefaultCloudPrefix
	} else if err := validateCloudPrefix(prefix); err != nil {
		return nil, err
	}
	return &PlatformCloud{
		cx:       cx,
		scopeCtx: cx.Context(),
		provider: CloudProviderFor(core.Untracked(cx, cx.Env().OS)),
		prefix:   prefix,
	}, nil
}

func validateCloudPrefix(prefix string) error {
	if strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, "\\\x00") || strings.Contains(prefix, "..") || strings.Contains(prefix, "//") {
		return fmt.Errorf("liveops: invalid cloud prefix %q", prefix)
	}
	for _, r := range prefix {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("liveops: invalid cloud prefix %q", prefix)
		}
	}
	for _, seg := range strings.Split(prefix, "/") {
		if strings.HasPrefix(seg, ".") {
			return fmt.Errorf("liveops: invalid cloud prefix %q: path segment must not start with '.'", prefix)
		}
	}
	sample := prefix + "slot.json"
	if path.Clean(sample) != sample {
		return fmt.Errorf("liveops: invalid cloud prefix %q", prefix)
	}
	return nil
}

// Provider returns the built-in OS cloud-save service of the platform cx runs on (CloudProviderICloud, CloudProviderPlayGames, or CloudProviderNone), whether or not the player is signed in.
func (c *PlatformCloud) Provider() CloudProvider {
	return CloudProviderFor(core.Untracked(c.cx, c.cx.Env().OS))
}

// Available reports whether platform cloud storage is reachable right now (cx.Env().CanUseCloudStorage()). Read in a reactive computation on the engine goroutine, it updates when availability changes.
func (c *PlatformCloud) Available() bool {
	return c.cx.Env().CanUseCloudStorage()
}

// Prefix returns the file-name prefix prepended to each slot name.
func (c *PlatformCloud) Prefix() string { return c.prefix }

// FileName returns the platform cloud file or snapshot name for slot ("<Prefix><slot>.json"), or an error wrapping save.ErrInvalidSlot when slot is invalid.
func (c *PlatformCloud) FileName(slot string) (string, error) {
	if err := save.ValidateSlot(slot); err != nil {
		return "", fmt.Errorf("liveops: %w", err)
	}
	return c.prefix + slot + ".json", nil
}

func (c *PlatformCloud) slotFromFile(name string) (string, bool) {
	rest, ok := strings.CutPrefix(name, c.prefix)
	if !ok {
		return "", false
	}
	slot, ok := strings.CutSuffix(rest, ".json")
	if !ok {
		return "", false
	}
	if save.ValidateSlot(slot) != nil {
		return "", false
	}
	return slot, true
}

// Load implements CloudSync. It returns ErrSlotNotFound when slot does not exist, ErrCloudUnavailable when cloud storage is unavailable or cx is disposed, and ErrCloudConflict (also wrapping *cloud.ConflictError) when unresolved native platform conflicts exist.
func (c *PlatformCloud) Load(ctx context.Context, slot string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, err := c.FileName(slot)
	if err != nil {
		return nil, err
	}
	if c.cx.Disposed() {
		return nil, mapCloudError("load", slot, cloud.ErrUnavailable)
	}
	if store := c.cx.CloudStore(); store != nil {
		data, err := store.ReadFile(name)
		if err != nil {
			return nil, mapCloudError("load", slot, err)
		}
		return bytes.Clone(data), nil
	}
	if c.provider == CloudProviderNone {
		return nil, mapCloudError("load", slot, cloud.ErrUnavailable)
	}
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	cloud.ReadFile(c.cx, name, func(data []byte, err error) {
		ch <- result{data, err}
	})
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, mapCloudError("load", slot, r.err)
		}
		return r.data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.scopeCtx.Done():
		return nil, mapCloudError("load", slot, cloud.ErrUnavailable)
	}
}

// Save implements CloudSync. It writes data to slot and returns ErrCloudUnavailable rather than silently dropping writes when cloud storage is unavailable.
func (c *PlatformCloud) Save(ctx context.Context, slot string, data []byte) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	name, err := c.FileName(slot)
	if err != nil {
		return err
	}
	if c.cx.Disposed() {
		return mapCloudError("save", slot, cloud.ErrUnavailable)
	}
	if store := c.cx.CloudStore(); store != nil {
		return mapCloudError("save", slot, store.WriteFile(name, bytes.Clone(data)))
	}
	if c.provider == CloudProviderNone {
		return mapCloudError("save", slot, cloud.ErrUnavailable)
	}
	ch := make(chan error, 1)
	cloud.WriteFile(c.cx, name, data, func(err error) {
		ch <- err
	})
	select {
	case err := <-ch:
		return mapCloudError("save", slot, err)
	case <-ctx.Done():
		return ctx.Err()
	case <-c.scopeCtx.Done():
		return mapCloudError("save", slot, cloud.ErrUnavailable)
	}
}

// Delete implements CloudSync. Deleting an absent slot succeeds when cloud storage is available and returns ErrCloudUnavailable when it is not.
func (c *PlatformCloud) Delete(ctx context.Context, slot string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	name, err := c.FileName(slot)
	if err != nil {
		return err
	}
	if c.cx.Disposed() {
		return mapCloudError("delete", slot, cloud.ErrUnavailable)
	}
	if store := c.cx.CloudStore(); store != nil {
		return mapCloudError("delete", slot, store.DeleteFile(name))
	}
	if c.provider == CloudProviderNone {
		return mapCloudError("delete", slot, cloud.ErrUnavailable)
	}
	ch := make(chan error, 1)
	cloud.DeleteFile(c.cx, name, func(err error) {
		ch <- err
	})
	select {
	case err := <-ch:
		return mapCloudError("delete", slot, err)
	case <-ctx.Done():
		return ctx.Err()
	case <-c.scopeCtx.Done():
		return mapCloudError("delete", slot, cloud.ErrUnavailable)
	}
}

// List implements CloudSync. It lists the sorted names of valid save slots matching this PlatformCloud's prefix, ignoring other files in the cloud container.
func (c *PlatformCloud) List(ctx context.Context) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.cx.Disposed() {
		return nil, mapCloudError("list", "", cloud.ErrUnavailable)
	}
	var names []string
	if store := c.cx.CloudStore(); store != nil {
		var err error
		names, err = store.ListFiles()
		if err != nil {
			return nil, mapCloudError("list", "", err)
		}
	} else {
		if c.provider == CloudProviderNone {
			return nil, mapCloudError("list", "", cloud.ErrUnavailable)
		}
		type result struct {
			names []string
			err   error
		}
		ch := make(chan result, 1)
		cloud.ListFiles(c.cx, func(got []string, err error) {
			ch <- result{got, err}
		})
		select {
		case r := <-ch:
			if r.err != nil {
				return nil, mapCloudError("list", "", r.err)
			}
			names = r.names
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.scopeCtx.Done():
			return nil, mapCloudError("list", "", cloud.ErrUnavailable)
		}
	}
	var slots []string
	for _, name := range names {
		if slot, ok := c.slotFromFile(name); ok {
			slots = append(slots, slot)
		}
	}
	slices.Sort(slots)
	return slices.Compact(slots), nil
}

// WatchChanges subscribes onChange to external cloud-save changes for slots matching this PlatformCloud's prefix. Notifications are delivered on cx's engine goroutine and stop automatically when stop is called or cx is disposed. Account-change notifications (cloud.ChangeAccount) and container-wide initial-sync notifications are delivered even when the platform did not enumerate individual file names.
func (c *PlatformCloud) WatchChanges(onChange func(CloudChange)) (stop func()) {
	if onChange == nil || c.cx.Disposed() {
		return func() {}
	}
	return cloud.WatchChanges(c.cx, func(ev cloud.ChangeEvent) {
		var slots []string
		for _, name := range ev.Files {
			if slot, ok := c.slotFromFile(name); ok {
				slots = append(slots, slot)
			}
		}
		slices.Sort(slots)
		slots = slices.Compact(slots)
		if len(slots) == 0 {
			switch ev.Reason {
			case cloud.ChangeAccount:
				// Signed-in account changed: always notify so the game can re-sync.
			case cloud.ChangeInitialSync:
				if len(ev.Files) > 0 || len(ev.Keys) > 0 {
					return
				}
			default:
				return
			}
		}
		onChange(CloudChange{Slots: slots, Reason: ev.Reason})
	})
}

// OnConflict subscribes resolve to native platform version conflicts for slots matching this PlatformCloud's prefix. Notifications are delivered on cx's engine goroutine and stop automatically when stop is called or cx is disposed.
func (c *PlatformCloud) OnConflict(resolve func(PlatformConflict)) (stop func()) {
	if resolve == nil || c.cx.Disposed() {
		return func() {}
	}
	return cloud.OnConflict(c.cx, func(pc cloud.Conflict) {
		slot, ok := c.slotFromFile(pc.Name)
		if !ok {
			return
		}
		resolveFn := pc.Resolve
		resolve(PlatformConflict{
			Slot:       slot,
			Local:      pc.Local,
			Remote:     pc.Remote,
			LocalTime:  pc.LocalTime,
			RemoteTime: pc.RemoteTime,
			Resolve: func(chosen []byte) error {
				if resolveFn == nil {
					return mapCloudError("resolve conflict", slot, cloud.ErrUnavailable)
				}
				return mapCloudError("resolve conflict", slot, resolveFn(chosen))
			},
		})
	})
}

func mapCloudError(op, slot string, err error) error {
	if err == nil {
		return nil
	}
	what := op
	if slot != "" {
		what = fmt.Sprintf("%s %q", op, slot)
	}
	switch {
	case errors.Is(err, cloud.ErrUnavailable):
		return fmt.Errorf("liveops: %s: %w: %w", what, ErrCloudUnavailable, err)
	case errors.Is(err, cloud.ErrNotFound):
		return fmt.Errorf("liveops: %s: %w: %w", what, ErrSlotNotFound, err)
	case errors.Is(err, cloud.ErrConflict):
		return fmt.Errorf("liveops: %s: %w: %w", what, ErrCloudConflict, err)
	default:
		return fmt.Errorf("liveops: %s: %w", what, err)
	}
}
