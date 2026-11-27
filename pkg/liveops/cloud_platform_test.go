package liveops_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/liveops"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/cloud"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestPlatformCloudProviderForAndStrings(t *testing.T) {
	cases := []struct {
		os   core.OS
		want liveops.CloudProvider
		str  string
	}{
		{core.OSMacOS, liveops.CloudProviderICloud, "icloud"},
		{core.OSIOS, liveops.CloudProviderICloud, "icloud"},
		{core.OSAndroid, liveops.CloudProviderPlayGames, "play-games"},
		{core.OSWindows, liveops.CloudProviderNone, "none"},
		{core.OSLinux, liveops.CloudProviderNone, "none"},
		{core.OSWeb, liveops.CloudProviderNone, "none"},
		{core.OS(""), liveops.CloudProviderNone, "none"},
	}
	for _, tc := range cases {
		got := liveops.CloudProviderFor(tc.os)
		if got != tc.want || got.String() != tc.str {
			t.Errorf("CloudProviderFor(%v) = %v (%q), want %v (%q)", tc.os, got, got.String(), tc.want, tc.str)
		}
	}
	if got := liveops.CloudProviderSteam.String(); got != "steam" {
		t.Errorf("CloudProviderSteam.String() = %q, want %q", got, "steam")
	}
}

type platformDevice struct {
	app    *nsgotest.App
	cx     *core.Scope
	cloud  *liveops.PlatformCloud
	store  *save.Store
	syncer *liveops.CloudSyncer
	now    time.Time
}

func newPlatformDevice(t *testing.T, id string, osKind core.OS, available bool, opts liveops.PlatformCloudOptions, mutate func(*liveops.CloudSyncOptions)) *platformDevice {
	t.Helper()
	d := &platformDevice{now: epoch}
	d.app = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		d.cx = cx
		return core.Text{
			TestID: "cloud-status",
			ContentFunc: func() string {
				if d.cloud != nil && d.cloud.Available() {
					return "available"
				}
				if cx.Env().CanUseCloudStorage() {
					return "available"
				}
				return "unavailable"
			},
		}
	}, nsgotest.Environment(core.Environment{OS: osKind}), nsgotest.CloudAvailable(available))

	pc, err := liveops.NewPlatformCloud(d.cx, opts)
	if err != nil {
		t.Fatalf("NewPlatformCloud: %v", err)
	}
	d.cloud = pc

	syncOpts := liveops.CloudSyncOptions{
		Local:  save.StorageBackend(d.app.Storage(), "save"),
		Cloud:  pc,
		State:  d.app.Storage(),
		Device: id,
		Now:    func() time.Time { return d.now },
	}
	if mutate != nil {
		mutate(&syncOpts)
	}
	s, err := liveops.NewCloudSyncer(syncOpts)
	if err != nil {
		t.Fatalf("NewCloudSyncer: %v", err)
	}
	d.syncer = s
	d.store = save.NewStore(s.Backend())
	return d
}

func (d *platformDevice) save(t *testing.T, slot string, p progress, at time.Duration) {
	t.Helper()
	d.now = epoch.Add(at)
	if err := d.store.Save(slot, p); err != nil {
		t.Fatalf("save %q: %v", slot, err)
	}
}

func (d *platformDevice) load(t *testing.T, slot string) progress {
	t.Helper()
	var p progress
	if err := d.store.Load(slot, &p); err != nil {
		t.Fatalf("load %q: %v", slot, err)
	}
	return p
}

// A save written on one device syncs through PlatformCloud and loads on another device, for both iCloud (iOS -> macOS) and Google Play Games (Android -> Android).
func TestPlatformCloudDeviceToDeviceSync(t *testing.T) {
	services := []struct {
		name         string
		osA, osB     core.OS
		wantProvider liveops.CloudProvider
	}{
		{"iCloud", core.OSIOS, core.OSMacOS, liveops.CloudProviderICloud},
		{"PlayGames", core.OSAndroid, core.OSAndroid, liveops.CloudProviderPlayGames},
	}

	for _, svc := range services {
		t.Run(svc.name, func(t *testing.T) {
			devA := newPlatformDevice(t, "device-a", svc.osA, true, liveops.PlatformCloudOptions{}, nil)
			devB := newPlatformDevice(t, "device-b", svc.osB, true, liveops.PlatformCloudOptions{}, nil)

			if devA.cloud.Provider() != svc.wantProvider || devB.cloud.Provider() != svc.wantProvider {
				t.Fatalf("providers = %v, %v; want %v", devA.cloud.Provider(), devB.cloud.Provider(), svc.wantProvider)
			}
			if !devA.cloud.Available() || !devB.cloud.Available() {
				t.Fatal("expected platform cloud available on both devices")
			}

			var eventsB []liveops.CloudChange
			stopB := devB.cloud.WatchChanges(func(ev liveops.CloudChange) {
				eventsB = append(eventsB, ev)
			})
			defer stopB()

			// Device A saves and uploads.
			devA.save(t, "main", progress{Level: 9, Note: svc.name}, time.Minute)
			resA, err := devA.syncer.Sync(context.Background())
			if err != nil {
				t.Fatalf("device A sync: %v", err)
			}
			if got := outcomeOf(t, resA, "main").Outcome; got != liveops.OutcomeUploaded {
				t.Fatalf("device A outcome = %v, want uploaded", got)
			}

			// File name in container must be flat (no slash) so Play Games and iCloud both accept it.
			fileName, err := devA.cloud.FileName("main")
			if err != nil || fileName != "save.main.json" {
				t.Fatalf("FileName(main) = %q, %v; want save.main.json", fileName, err)
			}
			payload, err := devA.cx.CloudStore().ReadFile(fileName)
			if err != nil {
				t.Fatalf("read uploaded cloud file %q: %v", fileName, err)
			}

			// Propagate the cloud file to device B as the platform service does.
			devB.app.SimulateCloudChange(nil, map[string][]byte{fileName: payload}, cloud.ChangeServer)
			if len(eventsB) != 1 || !slices.Equal(eventsB[0].Slots, []string{"main"}) || eventsB[0].Reason != cloud.ChangeServer {
				t.Fatalf("device B change events = %+v", eventsB)
			}

			// Device B syncs and loads Device A's save.
			resB, err := devB.syncer.Sync(context.Background())
			if err != nil {
				t.Fatalf("device B sync: %v", err)
			}
			if got := outcomeOf(t, resB, "main").Outcome; got != liveops.OutcomeDownloaded {
				t.Fatalf("device B outcome = %v, want downloaded", got)
			}
			if got := devB.load(t, "main"); got != (progress{Level: 9, Note: svc.name}) {
				t.Fatalf("device B loaded = %+v, want level 9 %q", got, svc.name)
			}

			// Device B deletes the slot; deletion propagates back to Device A.
			if err := devB.store.Delete("main"); err != nil {
				t.Fatal(err)
			}
			resB, err = devB.syncer.Sync(context.Background())
			if err != nil {
				t.Fatalf("device B delete sync: %v", err)
			}
			if got := outcomeOf(t, resB, "main").Outcome; got != liveops.OutcomeDeletedCloud {
				t.Fatalf("device B delete outcome = %v, want deleted-cloud", got)
			}
			if _, err := devB.cloud.Load(context.Background(), "main"); !errors.Is(err, liveops.ErrSlotNotFound) {
				t.Fatalf("load deleted slot = %v, want ErrSlotNotFound", err)
			}

			devA.app.SimulateCloudChange(nil, map[string][]byte{fileName: nil}, cloud.ChangeServer)
			resA, err = devA.syncer.Sync(context.Background())
			if err != nil {
				t.Fatalf("device A delete sync: %v", err)
			}
			if got := outcomeOf(t, resA, "main").Outcome; got != liveops.OutcomeDeletedLocal {
				t.Fatalf("device A delete outcome = %v, want deleted-local", got)
			}
			var p progress
			if err := devA.store.Load("main", &p); !errors.Is(err, save.ErrNotFound) {
				t.Fatalf("device A load after delete = %v, want save.ErrNotFound", err)
			}
		})
	}
}

func TestPlatformCloudNativeConflictAndResolution(t *testing.T) {
	dev := newPlatformDevice(t, "iphone", core.OSIOS, true, liveops.PlatformCloudOptions{}, nil)

	var conflicts []liveops.PlatformConflict
	stop := dev.cloud.OnConflict(func(c liveops.PlatformConflict) {
		conflicts = append(conflicts, c)
	})
	defer stop()

	// Write two slots and sync them up.
	dev.save(t, "main", progress{Level: 4, Note: "iphone"}, 0)
	dev.save(t, "side", progress{Level: 2, Note: "side"}, 0)
	if _, err := dev.syncer.Sync(context.Background()); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	// Build a conflicting remote envelope for "main" (written later on "ipad").
	remoteSave, err := json.Marshal(struct {
		Version int      `json:"version"`
		Data    progress `json:"data"`
	}{Version: 1, Data: progress{Level: 8, Note: "ipad"}})
	if err != nil {
		t.Fatal(err)
	}
	remoteEnv, err := liveops.EncodeCloudSave(liveops.SaveVersion{
		Exists:   true,
		Revision: 2,
		Modified: epoch.Add(time.Hour),
		Device:   "ipad",
		Data:     remoteSave,
	})
	if err != nil {
		t.Fatal(err)
	}

	dev.app.Advance(10 * time.Second)
	// Unrelated file conflict must be ignored by OnConflict.
	dev.app.SimulateCloudConflict("other.txt", []byte("unrelated"))
	if len(conflicts) != 0 {
		t.Fatalf("unrelated conflict delivered: %+v", conflicts)
	}

	// Conflict on "save.main.json" fires OnConflict with decoded slot name "main".
	dev.app.SimulateCloudConflict("save.main.json", remoteEnv)
	if len(conflicts) != 1 {
		t.Fatalf("got %d conflicts, want 1", len(conflicts))
	}
	c := conflicts[0]
	if c.Slot != "main" {
		t.Fatalf("conflict slot = %q, want main", c.Slot)
	}
	if !c.RemoteTime.After(c.LocalTime) {
		t.Errorf("expected RemoteTime (%v) > LocalTime (%v)", c.RemoteTime, c.LocalTime)
	}
	localVer, err := liveops.DecodeCloudSave(c.Local)
	if err != nil || localVer.Device != "iphone" {
		t.Fatalf("decode local conflict payload: %+v, %v", localVer, err)
	}
	remoteVer, err := liveops.DecodeCloudSave(c.Remote)
	if err != nil || remoteVer.Device != "ipad" {
		t.Fatalf("decode remote conflict payload: %+v, %v", remoteVer, err)
	}

	// While unresolved, Load wraps ErrCloudConflict, cloud.ErrConflict and *cloud.ConflictError.
	_, err = dev.cloud.Load(context.Background(), "main")
	if !errors.Is(err, liveops.ErrCloudConflict) || !errors.Is(err, cloud.ErrConflict) {
		t.Fatalf("Load during conflict = %v, want ErrCloudConflict and cloud.ErrConflict", err)
	}
	var ce *cloud.ConflictError
	if !errors.As(err, &ce) || ce.Key != "save.main.json" {
		t.Fatalf("ConflictError = %+v", ce)
	}

	// CloudSyncer skips the conflicted slot and still syncs other slots cleanly.
	dev.save(t, "side", progress{Level: 3, Note: "side-updated"}, 2*time.Minute)
	res, syncErr := dev.syncer.Sync(context.Background())
	if !errors.Is(syncErr, liveops.ErrCloudConflict) {
		t.Fatalf("sync during conflict = %v, want ErrCloudConflict", syncErr)
	}
	if got := outcomeOf(t, res, "side").Outcome; got != liveops.OutcomeUploaded {
		t.Fatalf("side outcome = %v, want uploaded", got)
	}

	// Resolve with the newer remote envelope; next sync downloads it into local storage.
	if err := c.Resolve(c.Remote); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	res, err = dev.syncer.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync after resolve: %v", err)
	}
	if got := outcomeOf(t, res, "main").Outcome; got != liveops.OutcomeDownloaded {
		t.Fatalf("main outcome after resolve = %v, want downloaded", got)
	}
	if got := dev.load(t, "main"); got != (progress{Level: 8, Note: "ipad"}) {
		t.Fatalf("main after resolve = %+v", got)
	}

	// Calling Resolve while cloud is unavailable reports ErrCloudUnavailable.
	dev.app.SetCloudAvailable(false)
	if err := c.Resolve(c.Local); !errors.Is(err, liveops.ErrCloudUnavailable) || !errors.Is(err, cloud.ErrUnavailable) {
		t.Fatalf("Resolve while unavailable = %v, want ErrCloudUnavailable", err)
	}
}

func TestPlatformCloudUnavailableNeverSilentlyDropsWrites(t *testing.T) {
	dev := newPlatformDevice(t, "mac", core.OSMacOS, false, liveops.PlatformCloudOptions{}, nil)
	ctx := context.Background()

	if dev.cloud.Available() {
		t.Fatal("expected Available() == false initially")
	}
	if got := dev.app.Find("id:cloud-status").Text; got != "unavailable" {
		t.Fatalf("reactive status = %q, want unavailable", got)
	}

	checkUnavailable := func(stage string) {
		t.Helper()
		if _, err := dev.cloud.Load(ctx, "main"); !errors.Is(err, liveops.ErrCloudUnavailable) || !errors.Is(err, cloud.ErrUnavailable) {
			t.Errorf("%s: Load = %v, want ErrCloudUnavailable", stage, err)
		}
		if err := dev.cloud.Save(ctx, "main", []byte("dropped")); !errors.Is(err, liveops.ErrCloudUnavailable) || !errors.Is(err, cloud.ErrUnavailable) {
			t.Errorf("%s: Save = %v, want ErrCloudUnavailable", stage, err)
		}
		if err := dev.cloud.Delete(ctx, "main"); !errors.Is(err, liveops.ErrCloudUnavailable) || !errors.Is(err, cloud.ErrUnavailable) {
			t.Errorf("%s: Delete = %v, want ErrCloudUnavailable", stage, err)
		}
		if _, err := dev.cloud.List(ctx); !errors.Is(err, liveops.ErrCloudUnavailable) || !errors.Is(err, cloud.ErrUnavailable) {
			t.Errorf("%s: List = %v, want ErrCloudUnavailable", stage, err)
		}
	}

	checkUnavailable("initial")

	// Local save stays intact when sync fails offline.
	dev.save(t, "main", progress{Level: 5, Note: "offline"}, 0)
	if _, err := dev.syncer.Sync(ctx); !errors.Is(err, liveops.ErrCloudUnavailable) {
		t.Fatalf("offline sync = %v, want ErrCloudUnavailable", err)
	}

	// Sign in / become available: reactive status updates and sync uploads.
	dev.app.SetCloudAvailable(true)
	if !dev.cloud.Available() {
		t.Fatal("expected Available() == true after SetCloudAvailable(true)")
	}
	if got := dev.app.Find("id:cloud-status").Text; got != "available" {
		t.Fatalf("reactive status = %q, want available", got)
	}
	if err := dev.cloud.Save(ctx, "raw", []byte("original")); err != nil {
		t.Fatalf("Save while available: %v", err)
	}

	// Go offline again: writes must fail loudly and never overwrite "original".
	dev.app.SetCloudAvailable(false)
	if got := dev.app.Find("id:cloud-status").Text; got != "unavailable" {
		t.Fatalf("reactive status = %q, want unavailable", got)
	}
	checkUnavailable("after-disable")
	if err := dev.cloud.Save(ctx, "raw", []byte("clobber")); !errors.Is(err, liveops.ErrCloudUnavailable) {
		t.Fatalf("Save raw while offline = %v", err)
	}

	// Re-enable and verify original payload is untouched.
	dev.app.SetCloudAvailable(true)
	got, err := dev.cloud.Load(ctx, "raw")
	if err != nil || string(got) != "original" {
		t.Fatalf("Load after re-enable = %q, %v; want original, nil", got, err)
	}
}

func TestPlatformCloudWatchChangesFilteringAndCleanup(t *testing.T) {
	var showChild *core.Signal[bool]
	var pc *liveops.PlatformCloud
	var events []liveops.CloudChange

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		showChild = core.NewSignal(cx, true)
		return core.Show{
			When: showChild.Get,
			Then: func(child *core.Scope) core.Node {
				var err error
				pc, err = liveops.NewPlatformCloud(child, liveops.PlatformCloudOptions{})
				if err != nil {
					t.Errorf("NewPlatformCloud: %v", err)
					return core.View{}
				}
				pc.WatchChanges(func(ev liveops.CloudChange) {
					events = append(events, ev)
				})
				return core.View{TestID: "child"}
			},
		}
	}, nsgotest.CloudAvailable(true))

	// KV-only and unrelated file changes are ignored.
	app.SimulateCloudChange(map[string][]byte{"theme": []byte("dark")}, nil, cloud.ChangeServer)
	app.SimulateCloudChange(nil, map[string][]byte{"notes.txt": []byte("hi"), "save.settings.json": []byte("{}")}, cloud.ChangeServer)
	if len(events) != 0 {
		t.Fatalf("unexpected events for unrelated changes: %+v", events)
	}

	// Matching slots are decoded and sorted.
	app.SimulateCloudChange(nil, map[string][]byte{
		"save.slot2.json": []byte("b"),
		"save.slot1.json": []byte("a"),
		"other.json":      []byte("ignore"),
	}, cloud.ChangeServer)
	if len(events) != 1 || !slices.Equal(events[0].Slots, []string{"slot1", "slot2"}) || events[0].Reason != cloud.ChangeServer {
		t.Fatalf("events = %+v", events)
	}

	// Account-change notifications arrive even with no files listed.
	app.SimulateCloudChange(nil, nil, cloud.ChangeAccount)
	if len(events) != 2 || len(events[1].Slots) != 0 || events[1].Reason != cloud.ChangeAccount {
		t.Fatalf("account event = %+v", events)
	}

	// Disposing child scope stops further notifications and marks PlatformCloud unavailable.
	app.Post(func() { showChild.Set(false) })
	app.Settle()

	app.SimulateCloudChange(nil, map[string][]byte{"save.slot1.json": []byte("after-dispose")}, cloud.ChangeServer)
	if len(events) != 2 {
		t.Fatalf("disposed watcher still fired: %+v", events)
	}
	if _, err := pc.Load(context.Background(), "slot1"); !errors.Is(err, liveops.ErrCloudUnavailable) {
		t.Fatalf("Load on disposed scope = %v, want ErrCloudUnavailable", err)
	}
}

func TestPlatformCloudValidationPrefixAndUnsupportedTargets(t *testing.T) {
	if _, err := liveops.NewPlatformCloud(nil, liveops.PlatformCloudOptions{}); err == nil {
		t.Fatal("expected error for nil scope")
	}

	var root *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		root = cx
		return core.View{}
	}, nsgotest.CloudAvailable(true))

	for _, bad := range []string{".hidden.", "a/.b/", "/abs/", "../escape/", "a//b/", `a\b`, "bad\x00prefix"} {
		if _, err := liveops.NewPlatformCloud(root, liveops.PlatformCloudOptions{Prefix: bad}); err == nil {
			t.Errorf("NewPlatformCloud(Prefix=%q) succeeded, want error", bad)
		}
	}

	pcA, err := liveops.NewPlatformCloud(root, liveops.PlatformCloudOptions{Prefix: "alpha."})
	if err != nil {
		t.Fatal(err)
	}
	pcB, err := liveops.NewPlatformCloud(root, liveops.PlatformCloudOptions{Prefix: "beta."})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := pcA.Save(ctx, "main", []byte("A")); err != nil {
		t.Fatal(err)
	}
	if err := pcB.Save(ctx, "main", []byte("B")); err != nil {
		t.Fatal(err)
	}
	// Extra non-save file in the same container must be ignored by List.
	if err := root.CloudStore().WriteFile("readme.txt", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	slotsA, err := pcA.List(ctx)
	if err != nil || !slices.Equal(slotsA, []string{"main"}) {
		t.Fatalf("pcA.List = %v, %v", slotsA, err)
	}
	gotA, err := pcA.Load(ctx, "main")
	if err != nil || string(gotA) != "A" {
		t.Fatalf("pcA.Load = %q, %v", gotA, err)
	}
	gotB, err := pcB.Load(ctx, "main")
	if err != nil || string(gotB) != "B" {
		t.Fatalf("pcB.Load = %q, %v", gotB, err)
	}

	// Invalid slot names are refused.
	for _, bad := range []string{"", "../x", "a/b", "settings"} {
		if _, err := pcA.Load(ctx, bad); !errors.Is(err, save.ErrInvalidSlot) {
			t.Errorf("Load(%q) = %v, want save.ErrInvalidSlot", bad, err)
		}
		if err := pcA.Save(ctx, bad, []byte("x")); !errors.Is(err, save.ErrInvalidSlot) {
			t.Errorf("Save(%q) = %v, want save.ErrInvalidSlot", bad, err)
		}
		if err := pcA.Delete(ctx, bad); !errors.Is(err, save.ErrInvalidSlot) {
			t.Errorf("Delete(%q) = %v, want save.ErrInvalidSlot", bad, err)
		}
	}

	// Cancelled context returns context.Canceled immediately.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := pcA.Load(cancelled, "main"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load(cancelled) = %v, want context.Canceled", err)
	}
	if err := pcA.Save(cancelled, "main", []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save(cancelled) = %v, want context.Canceled", err)
	}
	if err := pcA.Delete(cancelled, "main"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete(cancelled) = %v, want context.Canceled", err)
	}
	if _, err := pcA.List(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("List(cancelled) = %v, want context.Canceled", err)
	}

	// Unsupported target without a CloudStore fails immediately with ErrCloudUnavailable (never blocks).
	noCloudScope := core.NewRoot(core.RootOptions{})
	defer noCloudScope.Dispose()
	pcNone, err := liveops.NewPlatformCloud(noCloudScope, liveops.PlatformCloudOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pcNone.Provider() != liveops.CloudProviderNone {
		t.Fatalf("Provider = %v, want none", pcNone.Provider())
	}
	if _, err := pcNone.List(ctx); !errors.Is(err, liveops.ErrCloudUnavailable) {
		t.Fatalf("unsupported List = %v, want ErrCloudUnavailable", err)
	}
	if err := pcNone.Save(ctx, "main", []byte("x")); !errors.Is(err, liveops.ErrCloudUnavailable) {
		t.Fatalf("unsupported Save = %v, want ErrCloudUnavailable", err)
	}
	if _, err := pcNone.Load(ctx, "main"); !errors.Is(err, liveops.ErrCloudUnavailable) {
		t.Fatalf("unsupported Load = %v, want ErrCloudUnavailable", err)
	}
	if err := pcNone.Delete(ctx, "main"); !errors.Is(err, liveops.ErrCloudUnavailable) {
		t.Fatalf("unsupported Delete = %v, want ErrCloudUnavailable", err)
	}
	_ = app
}

// settleUntil pumps app.Settle until done is closed or timeout expires.
func settleUntil(t *testing.T, app *nsgotest.App, done <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		app.Settle()
		select {
		case <-done:
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for async sync callback")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCloudSyncerAsyncSyncResolveAndConcurrentLocalProtection(t *testing.T) {
	// Shared in-memory cloud store wrapped with a hook so we can inject a concurrent local save during cloud download.
	dir := t.TempDir()
	baseCloud, err := liveops.NewDirCloud(dir)
	if err != nil {
		t.Fatal(err)
	}
	var hookMu sync.Mutex
	var onLoad func(slot string)
	hookedCloud := &hookCloud{
		CloudSync: baseCloud,
		onLoad: func(slot string) {
			hookMu.Lock()
			fn := onLoad
			onLoad = nil
			hookMu.Unlock()
			if fn != nil {
				fn(slot)
			}
		},
	}

	var cxA, cxB *core.Scope
	appA := nsgotest.Start(t, func(cx *core.Scope) core.Node { cxA = cx; return core.View{} })
	appB := nsgotest.Start(t, func(cx *core.Scope) core.Node { cxB = cx; return core.View{} })

	nowA, nowB := epoch, epoch
	var deferred []liveops.Conflict
	syncerA, err := liveops.NewCloudSyncer(liveops.CloudSyncOptions{
		Local:  save.StorageBackend(appA.Storage(), "save"),
		Cloud:  baseCloud,
		State:  appA.Storage(),
		Device: "dev-a",
		Now:    func() time.Time { return nowA },
	})
	if err != nil {
		t.Fatal(err)
	}
	storeA := save.NewStore(syncerA.Backend())

	syncerB, err := liveops.NewCloudSyncer(liveops.CloudSyncOptions{
		Local:  save.StorageBackend(appB.Storage(), "save"),
		Cloud:  hookedCloud,
		State:  appB.Storage(),
		Device: "dev-b",
		Policy: liveops.PolicyAsk,
		Ask: func(_ context.Context, c liveops.Conflict) (liveops.Choice, error) {
			deferred = append(deferred, c)
			return liveops.ChoiceDefer, nil
		},
		Now: func() time.Time { return nowB },
	})
	if err != nil {
		t.Fatal(err)
	}
	storeB := save.NewStore(syncerB.Backend())

	// Device A uploads via SyncAsync.
	if err := storeA.Save("main", progress{Level: 1, Note: "start"}); err != nil {
		t.Fatal(err)
	}
	doneA := make(chan struct{})
	var resA liveops.SyncResult
	var errA error
	syncerA.SyncAsync(cxA, context.Background(), func(r liveops.SyncResult, e error) {
		resA, errA = r, e
		close(doneA)
	})
	settleUntil(t, appA, doneA)
	if errA != nil || outcomeOf(t, resA, "main").Outcome != liveops.OutcomeUploaded {
		t.Fatalf("syncA = %+v, %v", resA, errA)
	}

	// Device B downloads via SyncSlotAsync.
	doneB := make(chan struct{})
	var slotResB liveops.SlotResult
	var errB error
	syncerB.SyncSlotAsync(cxB, context.Background(), "main", func(r liveops.SlotResult, e error) {
		slotResB, errB = r, e
		close(doneB)
	})
	settleUntil(t, appB, doneB)
	if errB != nil || slotResB.Outcome != liveops.OutcomeDownloaded {
		t.Fatalf("syncSlotB = %+v, %v", slotResB, errB)
	}

	// Device A updates "main" to level 2 and uploads.
	nowA = epoch.Add(time.Minute)
	if err := storeA.Save("main", progress{Level: 2, Note: "a2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := syncerA.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	// While Device B is downloading level 2 asynchronously, the player saves level 99 locally on the engine goroutine.
	// SyncAsync must refuse to overwrite level 99 (ErrConflictChanged) rather than clobbering it!
	hookMu.Lock()
	onLoad = func(slot string) {
		if slot != "main" {
			return
		}
		saved := make(chan struct{})
		cxB.Post(func() {
			nowB = epoch.Add(2 * time.Minute)
			if err := storeB.Save("main", progress{Level: 99, Note: "local-during-download"}); err != nil {
				t.Errorf("concurrent local save: %v", err)
			}
			close(saved)
		})
		<-saved
	}
	hookMu.Unlock()

	doneRace := make(chan struct{})
	var raceErr error
	syncerB.SyncAsync(cxB, context.Background(), func(_ liveops.SyncResult, e error) {
		raceErr = e
		close(doneRace)
	})
	settleUntil(t, appB, doneRace)
	if !errors.Is(raceErr, liveops.ErrConflictChanged) {
		t.Fatalf("expected ErrConflictChanged on concurrent local save, got %v", raceErr)
	}
	var gotB progress
	if err := storeB.Load("main", &gotB); err != nil || gotB.Level != 99 {
		t.Fatalf("local save was clobbered! got %+v, %v", gotB, err)
	}

	// Next SyncAsync detects both copies as a conflict, defers via Ask, and ResolveAsync resolves it!
	doneConflict := make(chan struct{})
	var resConflict liveops.SyncResult
	var errConflict error
	syncerB.SyncAsync(cxB, context.Background(), func(r liveops.SyncResult, e error) {
		resConflict, errConflict = r, e
		close(doneConflict)
	})
	settleUntil(t, appB, doneConflict)
	if errConflict != nil || outcomeOf(t, resConflict, "main").Outcome != liveops.OutcomeDeferred || len(deferred) != 1 {
		t.Fatalf("conflict sync = %+v, err=%v, deferred=%d", resConflict, errConflict, len(deferred))
	}

	doneResolve := make(chan struct{})
	var resResolve liveops.SlotResult
	var errResolve error
	syncerB.ResolveAsync(cxB, context.Background(), deferred[0], liveops.ChoiceLocal, func(r liveops.SlotResult, e error) {
		resResolve, errResolve = r, e
		close(doneResolve)
	})
	settleUntil(t, appB, doneResolve)
	if errResolve != nil || resResolve.Outcome != liveops.OutcomeKeptLocal {
		t.Fatalf("ResolveAsync = %+v, %v", resResolve, errResolve)
	}

	// Device A now pulls Device B's winning level 99 save.
	if _, err := syncerA.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	var gotA progress
	if err := storeA.Load("main", &gotA); err != nil || gotA.Level != 99 {
		t.Fatalf("device A after resolve = %+v, %v", gotA, err)
	}

	// Nil scope calls done with error instead of panicking.
	var nilErr error
	syncerA.SyncAsync(nil, context.Background(), func(_ liveops.SyncResult, e error) { nilErr = e })
	if nilErr == nil {
		t.Fatal("expected error for nil scope in SyncAsync")
	}

	// Verify PlatformCloud also works cleanly via SyncAsync.
	devP := newPlatformDevice(t, "ipad", core.OSIOS, true, liveops.PlatformCloudOptions{}, nil)
	devP.save(t, "async-slot", progress{Level: 7, Note: "async"}, time.Minute)
	doneP := make(chan struct{})
	var resP liveops.SyncResult
	var errP error
	devP.syncer.SyncAsync(devP.cx, context.Background(), func(r liveops.SyncResult, e error) {
		resP, errP = r, e
		close(doneP)
	})
	settleUntil(t, devP.app, doneP)
	if errP != nil || outcomeOf(t, resP, "async-slot").Outcome != liveops.OutcomeUploaded {
		t.Fatalf("PlatformCloud SyncAsync = %+v, %v", resP, errP)
	}
	rawP, err := devP.cloud.Load(context.Background(), "async-slot")
	if err != nil {
		t.Fatalf("PlatformCloud Load after SyncAsync: %v", err)
	}
	vP, err := liveops.DecodeCloudSave(rawP)
	if err != nil || !bytes.Contains(vP.Data, []byte(`"async"`)) {
		t.Fatalf("PlatformCloud decoded payload after SyncAsync = %+v, %v", vP, err)
	}

	// Concurrent SyncAsync calls serialize cleanly without racing.
	done1, done2 := make(chan struct{}), make(chan struct{})
	var err1, err2 error
	devP.save(t, "async-slot", progress{Level: 8, Note: "second"}, 2*time.Minute)
	devP.syncer.SyncAsync(devP.cx, context.Background(), func(_ liveops.SyncResult, e error) { err1 = e; close(done1) })
	devP.syncer.SyncAsync(devP.cx, context.Background(), func(_ liveops.SyncResult, e error) { err2 = e; close(done2) })
	settleUntil(t, devP.app, done1)
	settleUntil(t, devP.app, done2)
	if err1 != nil || err2 != nil {
		t.Fatalf("concurrent SyncAsync errors: %v, %v", err1, err2)
	}

	// Cancelled context in SyncAsync delivers context.Canceled.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	doneCancel := make(chan struct{})
	var cancelErr error
	devP.syncer.SyncAsync(devP.cx, cancelled, func(_ liveops.SyncResult, e error) { cancelErr = e; close(doneCancel) })
	settleUntil(t, devP.app, doneCancel)
	if !errors.Is(cancelErr, context.Canceled) {
		t.Fatalf("cancelled SyncAsync = %v, want context.Canceled", cancelErr)
	}
}

type hookCloud struct {
	liveops.CloudSync
	onLoad func(slot string)
}

func (h *hookCloud) Load(ctx context.Context, slot string) ([]byte, error) {
	if h.onLoad != nil {
		h.onLoad(slot)
	}
	return h.CloudSync.Load(ctx, slot)
}
