package liveops_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/liveops"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// memCloud is an in-memory CloudSync shared by the simulated devices of a test.
type memCloud struct {
	slots map[string][]byte
	// beforeSave, when set, runs before each Save, so a test can model another device uploading in between.
	beforeSave func(slot string)
	// failSave, when set, makes Save of the slots it returns true for fail with ErrCloudUnavailable.
	failSave    func(slot string) bool
	unavailable bool
}

func newMemCloud() *memCloud { return &memCloud{slots: map[string][]byte{}} }

func (c *memCloud) Load(_ context.Context, slot string) ([]byte, error) {
	if c.unavailable {
		return nil, liveops.ErrCloudUnavailable
	}
	d, ok := c.slots[slot]
	if !ok {
		return nil, liveops.ErrSlotNotFound
	}
	return slices.Clone(d), nil
}

func (c *memCloud) Save(_ context.Context, slot string, data []byte) error {
	if c.unavailable || c.failSave != nil && c.failSave(slot) {
		return liveops.ErrCloudUnavailable
	}
	if hook := c.beforeSave; hook != nil {
		c.beforeSave = nil
		hook(slot)
	}
	c.slots[slot] = slices.Clone(data)
	return nil
}

func (c *memCloud) Delete(_ context.Context, slot string) error {
	if c.unavailable {
		return liveops.ErrCloudUnavailable
	}
	delete(c.slots, slot)
	return nil
}

func (c *memCloud) List(context.Context) ([]string, error) {
	if c.unavailable {
		return nil, liveops.ErrCloudUnavailable
	}
	var out []string
	for k := range c.slots {
		out = append(out, k)
	}
	slices.Sort(out)
	return out, nil
}

type progress struct {
	Level int    `json:"level"`
	Note  string `json:"note"`
}

// device is one simulated player device: its own app storage, save store and syncer, and a clock the test moves by hand.
type device struct {
	app    *nsgotest.App
	store  *save.Store
	syncer *liveops.CloudSyncer
	now    time.Time
}

var epoch = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func newDevice(t *testing.T, id string, cloud liveops.CloudSync, mutate func(*liveops.CloudSyncOptions)) *device {
	t.Helper()
	d := &device{now: epoch}
	d.app = nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	opts := liveops.CloudSyncOptions{
		Local:  save.StorageBackend(d.app.Storage(), "save"),
		Cloud:  cloud,
		State:  d.app.Storage(),
		Device: id,
		Now:    func() time.Time { return d.now },
		Summarize: func(slot string, data []byte) string {
			var env struct {
				Data progress `json:"data"`
			}
			if err := json.Unmarshal(data, &env); err != nil {
				return "unreadable"
			}
			return fmt.Sprintf("Level %d (%s)", env.Data.Level, env.Data.Note)
		},
	}
	if mutate != nil {
		mutate(&opts)
	}
	s, err := liveops.NewCloudSyncer(opts)
	if err != nil {
		t.Fatalf("NewCloudSyncer: %v", err)
	}
	d.syncer = s
	d.store = save.NewStore(s.Backend())
	return d
}

func (d *device) save(t *testing.T, slot string, p progress, at time.Duration) {
	t.Helper()
	d.now = epoch.Add(at)
	if err := d.store.Save(slot, p); err != nil {
		t.Fatalf("save %q: %v", slot, err)
	}
}

func (d *device) load(t *testing.T, slot string) progress {
	t.Helper()
	var p progress
	if err := d.store.Load(slot, &p); err != nil {
		t.Fatalf("load %q: %v", slot, err)
	}
	return p
}

func (d *device) sync(t *testing.T) liveops.SyncResult {
	t.Helper()
	res, err := d.syncer.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return res
}

func outcomeOf(t *testing.T, res liveops.SyncResult, slot string) liveops.SlotResult {
	t.Helper()
	for _, r := range res.Slots {
		if r.Slot == slot {
			return r
		}
	}
	t.Fatalf("no result for slot %q in %+v", slot, res.Slots)
	return liveops.SlotResult{}
}

func cloudProgress(t *testing.T, cloud *memCloud, slot string) (liveops.SaveVersion, progress) {
	t.Helper()
	raw, ok := cloud.slots[slot]
	if !ok {
		t.Fatalf("cloud has no slot %q", slot)
	}
	v, err := liveops.DecodeCloudSave(raw)
	if err != nil {
		t.Fatalf("decode cloud %q: %v", slot, err)
	}
	var env struct {
		Data progress `json:"data"`
	}
	if err := json.Unmarshal(v.Data, &env); err != nil {
		t.Fatalf("cloud %q data: %v", slot, err)
	}
	return v, env.Data
}

// syncedPair returns two devices that both hold slot "main" at level 1 and have synced it.
func syncedPair(t *testing.T, mutate func(*liveops.CloudSyncOptions)) (*memCloud, *device, *device) {
	t.Helper()
	cloud := newMemCloud()
	a := newDevice(t, "phone", cloud, mutate)
	b := newDevice(t, "tablet", cloud, mutate)
	a.save(t, "main", progress{Level: 1, Note: "start"}, 0)
	if got := outcomeOf(t, a.sync(t), "main").Outcome; got != liveops.OutcomeUploaded {
		t.Fatalf("first upload = %v", got)
	}
	if got := outcomeOf(t, b.sync(t), "main").Outcome; got != liveops.OutcomeDownloaded {
		t.Fatalf("first download = %v", got)
	}
	return cloud, a, b
}

// makeConflict has both devices change "main" after the shared sync; the phone uploads first, at the given offsets.
func makeConflict(t *testing.T, cloud *memCloud, a, b *device, phoneAt, tabletAt time.Duration) {
	t.Helper()
	a.save(t, "main", progress{Level: 5, Note: "phone"}, phoneAt)
	b.save(t, "main", progress{Level: 7, Note: "tablet"}, tabletAt)
	if got := outcomeOf(t, a.sync(t), "main").Outcome; got != liveops.OutcomeUploaded {
		t.Fatalf("phone upload = %v", got)
	}
	if v, p := cloudProgress(t, cloud, "main"); v.Device != "phone" || p.Level != 5 {
		t.Fatalf("cloud after phone upload: device %q %+v", v.Device, p)
	}
}

func TestCloudSyncPropagatesOneSidedChanges(t *testing.T) {
	cloud, a, b := syncedPair(t, nil)
	if got := b.load(t, "main"); got.Level != 1 {
		t.Fatalf("tablet level = %d", got.Level)
	}

	// Nothing changed: both sides up to date, no writes.
	before := string(cloud.slots["main"])
	if got := outcomeOf(t, a.sync(t), "main").Outcome; got != liveops.OutcomeUpToDate {
		t.Fatalf("idle sync = %v", got)
	}
	if string(cloud.slots["main"]) != before {
		t.Fatal("idle sync rewrote the cloud copy")
	}

	// Tablet progresses; phone picks it up without a conflict.
	b.save(t, "main", progress{Level: 2, Note: "tablet"}, time.Hour)
	if r := outcomeOf(t, b.sync(t), "main"); r.Outcome != liveops.OutcomeUploaded || r.Conflict != nil {
		t.Fatalf("tablet sync = %+v", r)
	}
	v, _ := cloudProgress(t, cloud, "main")
	if v.Revision != 2 || v.Device != "tablet" || !v.Modified.Equal(epoch.Add(time.Hour)) {
		t.Fatalf("cloud metadata = rev %d device %q modified %v", v.Revision, v.Device, v.Modified)
	}
	if r := outcomeOf(t, a.sync(t), "main"); r.Outcome != liveops.OutcomeDownloaded || r.Conflict != nil {
		t.Fatalf("phone sync = %+v", r)
	}
	if got := a.load(t, "main"); got.Level != 2 {
		t.Fatalf("phone level = %d", got.Level)
	}

	// A deletion on one device propagates to the cloud and then the other device.
	if err := a.store.Delete("main"); err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(t, a.sync(t), "main").Outcome; got != liveops.OutcomeDeletedCloud {
		t.Fatalf("phone delete sync = %v", got)
	}
	if _, ok := cloud.slots["main"]; ok {
		t.Fatal("cloud copy survived deletion")
	}
	if got := outcomeOf(t, b.sync(t), "main").Outcome; got != liveops.OutcomeDeletedLocal {
		t.Fatalf("tablet delete sync = %v", got)
	}
	if b.store.Exists("main") {
		t.Fatal("tablet copy survived deletion")
	}
	if res := b.sync(t); len(res.Slots) != 0 {
		t.Fatalf("sync after deletion still knows slots: %+v", res.Slots)
	}
}

func TestCloudSyncDetectsConflictFromTwoDevices(t *testing.T) {
	var seen []liveops.Conflict
	cloud, a, b := syncedPair(t, func(o *liveops.CloudSyncOptions) {
		o.Policy = liveops.PolicyAsk
		o.Ask = func(_ context.Context, c liveops.Conflict) (liveops.Choice, error) {
			seen = append(seen, c)
			return liveops.ChoiceDefer, nil
		}
	})
	makeConflict(t, cloud, a, b, time.Hour, 2*time.Hour)

	r := outcomeOf(t, b.sync(t), "main")
	if r.Outcome != liveops.OutcomeDeferred || r.Conflict == nil || len(seen) != 1 {
		t.Fatalf("tablet sync = %+v, asked %d times", r, len(seen))
	}
	c := seen[0]
	if c.Slot != "main" {
		t.Fatalf("conflict slot = %q", c.Slot)
	}
	if !c.Local.Exists || c.Local.Device != "tablet" || !c.Local.Modified.Equal(epoch.Add(2*time.Hour)) || c.Local.Summary != "Level 7 (tablet)" || c.Local.Revision != 1 {
		t.Fatalf("local side = %+v", c.Local)
	}
	if !c.Cloud.Exists || c.Cloud.Device != "phone" || !c.Cloud.Modified.Equal(epoch.Add(time.Hour)) || c.Cloud.Summary != "Level 5 (phone)" || c.Cloud.Revision != 2 {
		t.Fatalf("cloud side = %+v", c.Cloud)
	}

	// Deferring wrote nothing, and the next sync asks again.
	if got := b.load(t, "main"); got.Level != 7 {
		t.Fatalf("deferred conflict changed local save: %+v", got)
	}
	if _, p := cloudProgress(t, cloud, "main"); p.Level != 5 {
		t.Fatalf("deferred conflict changed cloud save: %+v", p)
	}
	if r := outcomeOf(t, b.sync(t), "main"); r.Outcome != liveops.OutcomeDeferred || len(seen) != 2 {
		t.Fatalf("second sync = %+v, asked %d times", r, len(seen))
	}
}

func TestCloudSyncPolicyNewest(t *testing.T) {
	t.Run("local newer wins", func(t *testing.T) {
		cloud, a, b := syncedPair(t, nil)
		makeConflict(t, cloud, a, b, time.Hour, 2*time.Hour)
		r := outcomeOf(t, b.sync(t), "main")
		if r.Outcome != liveops.OutcomeKeptLocal || r.Conflict == nil || r.Discarded == nil {
			t.Fatalf("tablet sync = %+v", r)
		}
		if !bytes.Contains(r.Discarded.Data, []byte(`"phone"`)) {
			t.Fatalf("discarded copy is not the phone's: %s", r.Discarded.Data)
		}
		v, p := cloudProgress(t, cloud, "main")
		if p.Level != 7 || v.Revision != 3 || v.Device != "tablet" {
			t.Fatalf("cloud = rev %d %+v", v.Revision, p)
		}
		if got := outcomeOf(t, a.sync(t), "main").Outcome; got != liveops.OutcomeDownloaded {
			t.Fatalf("phone follow-up = %v", got)
		}
		if got := a.load(t, "main"); got.Level != 7 {
			t.Fatalf("phone level = %d", got.Level)
		}
	})

	t.Run("cloud newer wins and local stays in backup", func(t *testing.T) {
		cloud, a, b := syncedPair(t, nil)
		makeConflict(t, cloud, a, b, 3*time.Hour, 2*time.Hour)
		r := outcomeOf(t, b.sync(t), "main")
		if r.Outcome != liveops.OutcomeKeptCloud || r.Discarded == nil || r.Discarded.Device != "tablet" {
			t.Fatalf("tablet sync = %+v", r)
		}
		if got := b.load(t, "main"); got.Level != 5 {
			t.Fatalf("tablet level = %d", got.Level)
		}
		backup, err := save.StorageBackend(b.app.Storage(), "save").ReadBackup("main")
		if err != nil || !bytes.Equal(backup, r.Discarded.Data) {
			t.Fatalf("tablet backup = %s, %v; want the discarded copy", backup, err)
		}
		if got := outcomeOf(t, b.sync(t), "main").Outcome; got != liveops.OutcomeUpToDate {
			t.Fatalf("resync = %v", got)
		}
	})

	t.Run("equal times keep both", func(t *testing.T) {
		cloud, a, b := syncedPair(t, nil)
		makeConflict(t, cloud, a, b, time.Hour, time.Hour)
		r := outcomeOf(t, b.sync(t), "main")
		if r.Outcome != liveops.OutcomeKeptBoth || r.Fork != "main.conflict-tablet" {
			t.Fatalf("tablet sync = %+v", r)
		}
	})

	t.Run("a write that bypassed Backend has an unknown time and keeps both", func(t *testing.T) {
		_, a, b := syncedPair(t, nil)
		a.save(t, "main", progress{Level: 5, Note: "phone"}, time.Hour)
		a.sync(t)
		// The tablet's last stamp is from its download at epoch; a later write that bypasses CloudSyncer.Backend must not inherit it.
		b.now = epoch.Add(5 * time.Hour)
		raw, _ := json.Marshal(map[string]any{"version": 1, "data": progress{Level: 9, Note: "raw"}})
		if err := save.StorageBackend(b.app.Storage(), "save").Write("main", raw); err != nil {
			t.Fatal(err)
		}
		r := outcomeOf(t, b.sync(t), "main")
		if r.Outcome != liveops.OutcomeKeptBoth || r.Conflict == nil || !r.Conflict.Local.Modified.IsZero() {
			t.Fatalf("tablet sync = %+v", r)
		}
		if got := b.load(t, r.Fork); got.Level != 9 {
			t.Fatalf("fork = %+v", got)
		}
	})

	t.Run("a bypassing write uploads with the sync time, not a stale stamp", func(t *testing.T) {
		cloud, _, b := syncedPair(t, nil)
		b.now = epoch.Add(3 * time.Hour)
		raw, _ := json.Marshal(map[string]any{"version": 1, "data": progress{Level: 4, Note: "raw"}})
		if err := save.StorageBackend(b.app.Storage(), "save").Write("main", raw); err != nil {
			t.Fatal(err)
		}
		if got := outcomeOf(t, b.sync(t), "main").Outcome; got != liveops.OutcomeUploaded {
			t.Fatalf("tablet sync = %v", got)
		}
		if v, _ := cloudProgress(t, cloud, "main"); !v.Modified.Equal(b.now) {
			t.Fatalf("uploaded modified = %v, want %v", v.Modified, b.now)
		}
	})

	t.Run("an edit beats a deletion", func(t *testing.T) {
		cloud, a, b := syncedPair(t, nil)
		if err := a.store.Delete("main"); err != nil {
			t.Fatal(err)
		}
		a.sync(t)
		b.save(t, "main", progress{Level: 3, Note: "tablet"}, 5*time.Minute)
		r := outcomeOf(t, b.sync(t), "main")
		if r.Outcome != liveops.OutcomeKeptLocal || r.Conflict == nil || r.Conflict.Cloud.Exists {
			t.Fatalf("tablet sync = %+v", r)
		}
		if _, p := cloudProgress(t, cloud, "main"); p.Level != 3 {
			t.Fatalf("cloud = %+v", p)
		}
	})
}

func TestCloudSyncPolicyKeepBoth(t *testing.T) {
	keepBoth := func(o *liveops.CloudSyncOptions) { o.Policy = liveops.PolicyKeepBoth }
	cloud, a, b := syncedPair(t, keepBoth)
	makeConflict(t, cloud, a, b, time.Hour, 2*time.Hour)

	r := outcomeOf(t, b.sync(t), "main")
	if r.Outcome != liveops.OutcomeKeptBoth || r.Fork != "main.conflict-tablet" || r.Discarded != nil {
		t.Fatalf("tablet sync = %+v", r)
	}
	if got := b.load(t, "main"); got.Level != 5 {
		t.Fatalf("tablet main = %+v", got)
	}
	if got := b.load(t, r.Fork); got.Level != 7 {
		t.Fatalf("tablet fork = %+v", got)
	}
	v, p := cloudProgress(t, cloud, r.Fork)
	if p.Level != 7 || v.Revision != 1 || v.Device != "tablet" || !v.Modified.Equal(epoch.Add(2*time.Hour)) {
		t.Fatalf("cloud fork = rev %d %+v", v.Revision, p)
	}

	// The other device receives the fork, and both are then in step.
	res := a.sync(t)
	if got := outcomeOf(t, res, r.Fork).Outcome; got != liveops.OutcomeDownloaded {
		t.Fatalf("phone fork sync = %v", got)
	}
	if got := a.load(t, r.Fork); got.Level != 7 {
		t.Fatalf("phone fork = %+v", got)
	}
	for _, d := range []*device{a, b} {
		var names []string
		for _, s := range d.sync(t).Slots {
			if s.Outcome != liveops.OutcomeUpToDate {
				t.Fatalf("not in step after keep-both: %+v", s)
			}
			names = append(names, s.Slot)
		}
		// Every result is a real slot, not an escaped storage key.
		if want := []string{"main", "main.conflict-tablet"}; !slices.Equal(names, want) {
			t.Fatalf("synced slots = %q, want %q", names, want)
		}
	}

	// A second conflict on the same slot gets a fresh fork name.
	a.save(t, "main", progress{Level: 10, Note: "phone"}, 3*time.Hour)
	b.save(t, "main", progress{Level: 11, Note: "tablet"}, 4*time.Hour)
	a.sync(t)
	if r := outcomeOf(t, b.sync(t), "main"); r.Fork != "main.conflict-tablet-2" {
		t.Fatalf("second fork = %+v", r)
	}
}

func TestCloudSyncPolicyAskChoices(t *testing.T) {
	cases := []struct {
		choice    liveops.Choice
		outcome   liveops.SyncOutcome
		local     int
		cloud     int
		wantFork  bool
		discarded string
	}{
		{liveops.ChoiceLocal, liveops.OutcomeKeptLocal, 7, 7, false, "phone"},
		{liveops.ChoiceCloud, liveops.OutcomeKeptCloud, 5, 5, false, "tablet"},
		{liveops.ChoiceBoth, liveops.OutcomeKeptBoth, 5, 5, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.choice.String(), func(t *testing.T) {
			cloud, a, b := syncedPair(t, func(o *liveops.CloudSyncOptions) {
				o.Policy = liveops.PolicyAsk
				o.Ask = func(context.Context, liveops.Conflict) (liveops.Choice, error) { return tc.choice, nil }
			})
			makeConflict(t, cloud, a, b, 2*time.Hour, time.Hour)
			r := outcomeOf(t, b.sync(t), "main")
			if r.Outcome != tc.outcome || (r.Fork != "") != tc.wantFork {
				t.Fatalf("sync = %+v", r)
			}
			if tc.discarded != "" && (r.Discarded == nil || !bytes.Contains(r.Discarded.Data, []byte(tc.discarded))) {
				t.Fatalf("discarded = %+v", r.Discarded)
			}
			if got := b.load(t, "main"); got.Level != tc.local {
				t.Fatalf("local = %+v", got)
			}
			if _, p := cloudProgress(t, cloud, "main"); p.Level != tc.cloud {
				t.Fatalf("cloud = %+v", p)
			}
			if got := outcomeOf(t, b.sync(t), "main").Outcome; got != liveops.OutcomeUpToDate {
				t.Fatalf("resync = %v", got)
			}
		})
	}

	t.Run("ask error leaves both sides", func(t *testing.T) {
		boom := errors.New("dialog closed")
		cloud, a, b := syncedPair(t, func(o *liveops.CloudSyncOptions) {
			o.Policy = liveops.PolicyAsk
			o.Ask = func(context.Context, liveops.Conflict) (liveops.Choice, error) { return 0, boom }
		})
		makeConflict(t, cloud, a, b, time.Hour, 2*time.Hour)
		_, err := b.syncer.Sync(context.Background())
		if !errors.Is(err, boom) {
			t.Fatalf("sync error = %v", err)
		}
		if got := b.load(t, "main"); got.Level != 7 {
			t.Fatalf("local = %+v", got)
		}
		if _, p := cloudProgress(t, cloud, "main"); p.Level != 5 {
			t.Fatalf("cloud = %+v", p)
		}
	})
}

func TestCloudSyncFirstSyncOnNewDevice(t *testing.T) {
	cloud := newMemCloud()
	a := newDevice(t, "phone", cloud, nil)
	a.save(t, "main", progress{Level: 4, Note: "phone"}, time.Hour)
	a.sync(t)

	// A device that played offline before ever syncing holds a different save: a conflict, not an overwrite.
	b := newDevice(t, "tablet", cloud, func(o *liveops.CloudSyncOptions) { o.Policy = liveops.PolicyKeepBoth })
	b.save(t, "main", progress{Level: 2, Note: "tablet"}, 30*time.Minute)
	r := outcomeOf(t, b.sync(t), "main")
	if r.Conflict == nil || r.Outcome != liveops.OutcomeKeptBoth || r.Conflict.Local.Revision != 0 {
		t.Fatalf("first sync = %+v", r)
	}

	// Identical copies on a fresh device are simply recorded.
	c := newDevice(t, "laptop", cloud, nil)
	raw := cloudBody(t, cloud, "main")
	if err := save.StorageBackend(c.app.Storage(), "save").Write("main", raw); err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(t, c.sync(t), "main").Outcome; got != liveops.OutcomeUpToDate {
		t.Fatalf("identical first sync = %v", got)
	}
}

func TestCloudSyncForkUploadFailureIsRetried(t *testing.T) {
	cloud, a, b := syncedPair(t, func(o *liveops.CloudSyncOptions) { o.Policy = liveops.PolicyKeepBoth })
	makeConflict(t, cloud, a, b, time.Hour, 2*time.Hour)

	cloud.failSave = func(slot string) bool { return strings.Contains(slot, ".conflict-") }
	if _, err := b.syncer.Sync(context.Background()); !errors.Is(err, liveops.ErrCloudUnavailable) {
		t.Fatalf("sync with failing fork upload = %v", err)
	}
	// The local copy is safe in the fork, and the slot itself is untouched.
	if got := b.load(t, "main.conflict-tablet"); got.Level != 7 {
		t.Fatalf("local fork = %+v", got)
	}
	if got := b.load(t, "main"); got.Level != 7 {
		t.Fatalf("main after failed fork = %+v", got)
	}

	// The next sync reuses the fork instead of creating "-2", and uploads it.
	cloud.failSave = nil
	res := b.sync(t)
	if r := outcomeOf(t, res, "main"); r.Outcome != liveops.OutcomeKeptBoth || r.Fork != "main.conflict-tablet" {
		t.Fatalf("retry = %+v", r)
	}
	if got := b.load(t, "main"); got.Level != 5 {
		t.Fatalf("main after retry = %+v", got)
	}
	if _, p := cloudProgress(t, cloud, "main.conflict-tablet"); p.Level != 7 {
		t.Fatalf("cloud fork = %+v", p)
	}
	var names []string
	for name := range cloud.slots {
		names = append(names, name)
	}
	slices.Sort(names)
	if want := []string{"main", "main.conflict-tablet"}; !slices.Equal(names, want) {
		t.Fatalf("cloud slots = %q, want %q", names, want)
	}
	for _, r := range b.sync(t).Slots {
		if r.Outcome != liveops.OutcomeUpToDate {
			t.Fatalf("not in step after retry: %+v", r)
		}
	}
}

func TestCloudSyncForkOfLongSlot(t *testing.T) {
	_, a, b := syncedPair(t, func(o *liveops.CloudSyncOptions) { o.Policy = liveops.PolicyKeepBoth })
	// Both names share their first 173 bytes, and the shortened fork name would cut the two-byte "é" in half.
	long1 := strings.Repeat("s", 173) + "é" + strings.Repeat("x", 20)
	long2 := strings.Repeat("s", 173) + "é" + strings.Repeat("y", 20)
	for _, slot := range []string{long1, long2} {
		a.save(t, slot, progress{Level: 1, Note: "start"}, 0)
	}
	a.sync(t)
	b.sync(t)
	for _, slot := range []string{long1, long2} {
		a.save(t, slot, progress{Level: 5, Note: "phone"}, time.Hour)
		b.save(t, slot, progress{Level: 7, Note: "tablet"}, 2*time.Hour)
	}
	a.sync(t)
	res := b.sync(t)
	f1, f2 := outcomeOf(t, res, long1).Fork, outcomeOf(t, res, long2).Fork
	for _, f := range []string{f1, f2} {
		if f == "" || len(f) > 200 || save.ValidateSlot(f) != nil || !strings.HasSuffix(f, ".conflict-tablet") {
			t.Fatalf("fork name %q (%d bytes)", f, len(f))
		}
		if got := b.load(t, f); got.Level != 7 {
			t.Fatalf("fork %q = %+v", f, got)
		}
	}
	if f1 == f2 {
		t.Fatalf("forks of different slots share the name %q", f1)
	}
}

func TestCloudSyncResolveLater(t *testing.T) {
	var pending []liveops.Conflict
	cloud, a, b := syncedPair(t, func(o *liveops.CloudSyncOptions) {
		o.Policy = liveops.PolicyAsk
		// The game queues the conflict for its dialog instead of blocking the sync.
		o.Ask = func(_ context.Context, c liveops.Conflict) (liveops.Choice, error) {
			pending = append(pending, c)
			return liveops.ChoiceDefer, nil
		}
	})
	makeConflict(t, cloud, a, b, time.Hour, 2*time.Hour)
	if r := outcomeOf(t, b.sync(t), "main"); r.Outcome != liveops.OutcomeDeferred || len(pending) != 1 {
		t.Fatalf("sync = %+v", r)
	}
	ctx := context.Background()
	c := pending[0]

	if _, err := b.syncer.Resolve(ctx, c, liveops.Choice(9)); err == nil {
		t.Fatal("Resolve accepted an unknown choice")
	}
	if r, err := b.syncer.Resolve(ctx, c, liveops.ChoiceDefer); err != nil || r.Outcome != liveops.OutcomeDeferred {
		t.Fatalf("Resolve defer = %+v, %v", r, err)
	}

	// The player kept playing while the dialog was open: the answer no longer applies.
	b.save(t, "main", progress{Level: 8, Note: "tablet"}, 3*time.Hour)
	if _, err := b.syncer.Resolve(ctx, c, liveops.ChoiceCloud); !errors.Is(err, liveops.ErrConflictChanged) {
		t.Fatalf("stale Resolve = %v", err)
	}
	if got := b.load(t, "main"); got.Level != 8 {
		t.Fatalf("stale Resolve wrote locally: %+v", got)
	}

	// Sync again, then answer the current conflict.
	b.sync(t)
	c = pending[len(pending)-1]
	r, err := b.syncer.Resolve(ctx, c, liveops.ChoiceLocal)
	if err != nil || r.Outcome != liveops.OutcomeKeptLocal || r.Discarded == nil || r.Conflict == nil || r.Conflict.Local.Summary != "Level 8 (tablet)" {
		t.Fatalf("Resolve local = %+v, %v", r, err)
	}
	if v, p := cloudProgress(t, cloud, "main"); p.Level != 8 || v.Device != "tablet" {
		t.Fatalf("cloud after Resolve = %+v", p)
	}
	if got := outcomeOf(t, b.sync(t), "main").Outcome; got != liveops.OutcomeUpToDate {
		t.Fatalf("sync after Resolve = %v", got)
	}

	// Resolving again is refused: both sides now hold the same copy.
	if _, err := b.syncer.Resolve(ctx, c, liveops.ChoiceCloud); !errors.Is(err, liveops.ErrConflictChanged) {
		t.Fatalf("second Resolve = %v", err)
	}
}

func TestCloudSyncLeavesSettingsAlone(t *testing.T) {
	cloud := newMemCloud()
	d := newDevice(t, "phone", cloud, nil)
	// Settings saved before the syncer was adopted.
	old := save.DefaultSettings()
	old.Locale = "ja"
	if err := save.NewStorageSettingsStore(d.app.Storage(), "save").Save(old); err != nil {
		t.Fatal(err)
	}
	settings := save.NewDefaultSettingsStore(d.syncer.Backend())
	got, err := settings.Load()
	if err != nil || got.Locale != "ja" {
		t.Fatalf("settings through syncer backend = %+v, %v", got, err)
	}
	got.Locale = "fr"
	if err := settings.Save(got); err != nil {
		t.Fatal(err)
	}
	d.save(t, "main", progress{Level: 1}, 0)

	// A stray state key for a name that is not a slot is ignored rather than failing every sync.
	_ = d.app.Storage().SetItem(liveops.DefaultSyncStateKey+".settings", `{"version":1,"revision":0}`)
	res := d.sync(t)
	if len(res.Slots) != 1 || res.Slots[0].Slot != "main" {
		t.Fatalf("synced slots = %+v", res.Slots)
	}
	if _, ok := cloud.slots["settings"]; ok {
		t.Fatal("settings uploaded as a save slot")
	}
	for _, k := range d.app.Storage().Keys() {
		if k == "save.slot.settings" {
			t.Fatalf("settings stored as a save slot: %q", d.app.Storage().Keys())
		}
	}
}

func cloudBody(t *testing.T, cloud *memCloud, slot string) []byte {
	t.Helper()
	v, err := liveops.DecodeCloudSave(cloud.slots[slot])
	if err != nil {
		t.Fatal(err)
	}
	return v.Data
}

func TestCloudSyncReadsPlainCloudSaves(t *testing.T) {
	cloud := newMemCloud()
	raw, _ := json.Marshal(map[string]any{"version": 1, "data": progress{Level: 6, Note: "legacy"}})
	cloud.slots["main"] = raw
	d := newDevice(t, "phone", cloud, nil)
	if got := outcomeOf(t, d.sync(t), "main").Outcome; got != liveops.OutcomeDownloaded {
		t.Fatalf("plain download = %v", got)
	}
	if got := d.load(t, "main"); got.Level != 6 {
		t.Fatalf("level = %d", got.Level)
	}
	d.save(t, "main", progress{Level: 8, Note: "upgraded"}, time.Hour)
	d.sync(t)
	if v, p := cloudProgress(t, cloud, "main"); v.Revision != 1 || p.Level != 8 {
		t.Fatalf("upgraded cloud = rev %d %+v", v.Revision, p)
	}
}

func TestCloudSyncRefusesCloudChangedMidSync(t *testing.T) {
	cloud, a, b := syncedPair(t, nil)
	a.save(t, "main", progress{Level: 2, Note: "phone"}, time.Hour)
	// The phone's upload re-reads the cloud just before writing; the tablet's upload lands in between.
	var hits int
	racing := &racingCloud{memCloud: cloud, onSecondLoad: func() {
		hits++
		b.save(t, "main", progress{Level: 3, Note: "tablet"}, 2*time.Hour)
		if r, err := b.syncer.SyncSlot(context.Background(), "main"); err != nil || r.Outcome != liveops.OutcomeUploaded {
			t.Fatalf("tablet upload = %+v, %v", r, err)
		}
	}}
	phone, err := liveops.NewCloudSyncer(liveops.CloudSyncOptions{
		Local: save.StorageBackend(a.app.Storage(), "save"), Cloud: racing, State: a.app.Storage(), Device: "phone",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := phone.SyncSlot(context.Background(), "main"); !errors.Is(err, liveops.ErrCloudChanged) || hits != 1 {
		t.Fatalf("racing upload error = %v (hits %d)", err, hits)
	}
	if v, p := cloudProgress(t, cloud, "main"); v.Device != "tablet" || p.Level != 3 {
		t.Fatalf("cloud overwritten by the losing upload: %+v %+v", v, p)
	}
	// The next sync sees the tablet's copy as a conflict instead of overwriting it.
	r, err := phone.SyncSlot(context.Background(), "main")
	if err != nil || r.Conflict == nil || r.Outcome != liveops.OutcomeKeptCloud {
		t.Fatalf("follow-up = %+v, %v", r, err)
	}
}

// racingCloud runs onSecondLoad on the second Load of a sync, between the syncer's first read and its pre-write check.
type racingCloud struct {
	*memCloud
	loads        int
	onSecondLoad func()
}

func (c *racingCloud) Load(ctx context.Context, slot string) ([]byte, error) {
	c.loads++
	if c.loads == 2 && c.onSecondLoad != nil {
		c.onSecondLoad()
	}
	return c.memCloud.Load(ctx, slot)
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestCloudSyncErrors(t *testing.T) {
	cloud := newMemCloud()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	base := liveops.CloudSyncOptions{Local: save.StorageBackend(app.Storage(), "save"), Cloud: cloud, State: app.Storage(), Device: "phone"}

	for name, mutate := range map[string]func(*liveops.CloudSyncOptions){
		"no local":     func(o *liveops.CloudSyncOptions) { o.Local = nil },
		"no cloud":     func(o *liveops.CloudSyncOptions) { o.Cloud = nil },
		"no state":     func(o *liveops.CloudSyncOptions) { o.State = core.Storage{} },
		"bad device":   func(o *liveops.CloudSyncOptions) { o.Device = "my phone" },
		"no device":    func(o *liveops.CloudSyncOptions) { o.Device = "" },
		"ask, no func": func(o *liveops.CloudSyncOptions) { o.Policy = liveops.PolicyAsk },
		"bad policy":   func(o *liveops.CloudSyncOptions) { o.Policy = 9 },
		"neg version":  func(o *liveops.CloudSyncOptions) { o.SaveVersion = -1 },
	} {
		o := base
		mutate(&o)
		if _, err := liveops.NewCloudSyncer(o); err == nil {
			t.Errorf("%s: NewCloudSyncer accepted %+v", name, o)
		}
	}

	s, err := liveops.NewCloudSyncer(base)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// A path-traversing cloud slot name is refused, not written locally, and the other slots still sync.
	cloud.slots["../escape"] = []byte(`{"version":1,"data":{}}`)
	cloud.slots["ok"] = []byte(`{"version":1,"data":{"level":1}}`)
	res, err := s.Sync(ctx)
	if !errors.Is(err, save.ErrInvalidSlot) {
		t.Fatalf("sync error = %v", err)
	}
	if outcomeOf(t, res, "ok").Outcome != liveops.OutcomeDownloaded {
		t.Fatalf("ok slot = %+v", res.Slots)
	}
	delete(cloud.slots, "../escape")

	// A corrupt cloud payload leaves the slot untouched.
	cloud.slots["bad"] = []byte(`{"format":"nsgo-cloud-save","version":1,"revision":1,"hash":"sha256:00","data":"x"}`)
	if _, err := s.SyncSlot(ctx, "bad"); !errors.Is(err, liveops.ErrCorruptCloudSave) {
		t.Fatalf("corrupt payload error = %v", err)
	}

	// Corrupt sync state is refused rather than read as "never synced".
	_ = app.Storage().SetItem(liveops.DefaultSyncStateKey+".ok", `{"version":1,"revision":1,"extra":true}`)
	if _, err := s.SyncSlot(ctx, "ok"); !errors.Is(err, liveops.ErrCorruptSyncState) {
		t.Fatalf("corrupt state error = %v", err)
	}

	// An unavailable cloud stops the sync at once.
	cloud.unavailable = true
	if _, err := s.Sync(ctx); !errors.Is(err, liveops.ErrCloudUnavailable) {
		t.Fatalf("offline sync error = %v", err)
	}
}

func TestCloudSaveEnvelopeRoundTrip(t *testing.T) {
	v := liveops.SaveVersion{Exists: true, Revision: 3, Modified: epoch.Add(90 * time.Second), Device: "phone", Data: []byte(`{"version":1,"data":{"note":"<a&b>"}}`)}
	raw, err := liveops.EncodeCloudSave(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "format": "nsgo-cloud-save",
  "version": 1,
  "revision": 3,
  "hash": "` + sha(v.Data) + `",
  "modified": "2026-11-01T12:01:30Z",
  "device": "phone",
  "data": "{\"version\":1,\"data\":{\"note\":\"<a&b>\"}}"
}
`
	if string(raw) != want {
		t.Fatalf("encoded =\n%s\nwant\n%s", raw, want)
	}
	got, err := liveops.DecodeCloudSave(raw)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := liveops.EncodeCloudSave(got)
	if !bytes.Equal(raw, again) || !bytes.Equal(got.Data, v.Data) || got.Revision != 3 || !got.Modified.Equal(v.Modified) {
		t.Fatalf("round trip changed the payload: %+v", got)
	}

	// Binary data goes through data_base64 and survives byte for byte.
	bin := liveops.SaveVersion{Revision: 1, Data: []byte{0xff, 0x00, 0xfe}}
	raw, err = liveops.EncodeCloudSave(bin)
	if err != nil || !bytes.Contains(raw, []byte(`"data_base64": "/wD+"`)) {
		t.Fatalf("binary encode = %s, %v", raw, err)
	}
	if got, err := liveops.DecodeCloudSave(raw); err != nil || !bytes.Equal(got.Data, bin.Data) {
		t.Fatalf("binary decode = %+v, %v", got, err)
	}

	for name, payload := range map[string]string{
		"unknown field":  strings.Replace(want, `"device"`, `"extra": 1, "device"`, 1),
		"newer version":  strings.Replace(want, `"version": 1`, `"version": 2`, 1),
		"zero revision":  strings.Replace(want, `"revision": 3`, `"revision": 0`, 1),
		"bad hash":       strings.Replace(want, sha(v.Data), "sha256:00", 1),
		"bad time":       strings.Replace(want, "2026-11-01T12:01:30Z", "yesterday", 1),
		"bad device":     strings.Replace(want, `"phone"`, `"my phone"`, 1),
		"both data":      strings.Replace(want, `"device"`, `"data_base64": "", "device"`, 1),
		"trailing value": want + "{}",
		"max revision":   strings.Replace(want, `"revision": 3`, `"revision": 9223372036854775807`, 1),
	} {
		if _, err := liveops.DecodeCloudSave([]byte(payload)); !errors.Is(err, liveops.ErrCorruptCloudSave) {
			t.Errorf("%s: decode error = %v", name, err)
		}
	}
	if _, err := liveops.EncodeCloudSave(liveops.SaveVersion{Revision: 0}); err == nil {
		t.Error("revision 0 encoded")
	}
}

func FuzzDecodeCloudSave(f *testing.F) {
	seed, _ := liveops.EncodeCloudSave(liveops.SaveVersion{Revision: 2, Modified: epoch, Device: "phone", Data: []byte(`{"version":1,"data":{}}`)})
	f.Add(seed)
	f.Add([]byte(`{"version":1,"data":{}}`))
	f.Add([]byte(`{"format":"nsgo-cloud-save"}`))
	f.Add([]byte{0xff, 0xfe})
	f.Fuzz(func(t *testing.T, raw []byte) {
		v, err := liveops.DecodeCloudSave(raw)
		if err != nil {
			return
		}
		if v.Revision == 0 {
			if !bytes.Equal(v.Data, raw) {
				t.Fatal("plain payload not returned as is")
			}
			return
		}
		enc, err := liveops.EncodeCloudSave(v)
		if err != nil {
			t.Fatalf("re-encode: %v", err)
		}
		back, err := liveops.DecodeCloudSave(enc)
		if err != nil || !bytes.Equal(back.Data, v.Data) || back.Revision != v.Revision || back.Hash != v.Hash || !back.Modified.Equal(v.Modified) || back.Device != v.Device {
			t.Fatalf("round trip: %+v, %v", back, err)
		}
		again, _ := liveops.EncodeCloudSave(back)
		if !bytes.Equal(enc, again) {
			t.Fatal("encoding not deterministic")
		}
	})
}

func BenchmarkCloudSyncUpToDate(b *testing.B) {
	cloud := newMemCloud()
	app := nsgotest.Start(b, func(cx *core.Scope) core.Node { return core.View{} })
	s, err := liveops.NewCloudSyncer(liveops.CloudSyncOptions{Local: save.StorageBackend(app.Storage(), "save"), Cloud: cloud, State: app.Storage(), Device: "bench"})
	if err != nil {
		b.Fatal(err)
	}
	store := save.NewStore(s.Backend())
	for i := range 16 {
		if err := store.Save(fmt.Sprintf("slot%02d", i), progress{Level: i, Note: strings.Repeat("x", 512)}); err != nil {
			b.Fatal(err)
		}
	}
	ctx := context.Background()
	if _, err := s.Sync(ctx); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Sync(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func newVersionedDevice(t *testing.T, id string, cloud liveops.CloudSync, saveVer int, mutate func(*liveops.CloudSyncOptions)) *device {
	t.Helper()
	d := &device{now: epoch}
	d.app = nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	reconfigureDevice(t, d, id, cloud, saveVer, mutate)
	return d
}

func reconfigureDevice(t *testing.T, d *device, id string, cloud liveops.CloudSync, saveVer int, mutate func(*liveops.CloudSyncOptions)) {
	t.Helper()
	opts := liveops.CloudSyncOptions{
		Local:       save.StorageBackend(d.app.Storage(), "save"),
		Cloud:       cloud,
		State:       d.app.Storage(),
		Device:      id,
		SaveVersion: saveVer,
		Now:         func() time.Time { return d.now },
		Summarize: func(slot string, data []byte) string {
			var env struct {
				Data progress `json:"data"`
			}
			if err := json.Unmarshal(data, &env); err != nil {
				return "unreadable"
			}
			return fmt.Sprintf("Level %d (%s)", env.Data.Level, env.Data.Note)
		},
	}
	if mutate != nil {
		mutate(&opts)
	}
	s, err := liveops.NewCloudSyncer(opts)
	if err != nil {
		t.Fatalf("NewCloudSyncer(v%d): %v", saveVer, err)
	}
	d.syncer = s
	storeOpts := []save.Option{save.WithVersion(saveVer)}
	for v := 1; v < saveVer; v++ {
		storeOpts = append(storeOpts, save.WithMigration(v, func(raw json.RawMessage) (json.RawMessage, error) {
			return raw, nil
		}))
	}
	d.store = save.NewStore(s.Backend(), storeOpts...)
}

func TestCloudSyncNewerVersionPreservesLocalAndCloud(t *testing.T) {
	// Issue #1538: phone updates from 1.2 (v2) to 1.3 (v3) while tablet stays on 1.2 (v2).
	cloud := newMemCloud()
	phone := newVersionedDevice(t, "phone", cloud, 2, nil)
	tablet := newVersionedDevice(t, "tablet", cloud, 2, nil)

	// Both devices start in step at save version 2.
	phone.save(t, "main", progress{Level: 4, Note: "shared-v2"}, 0)
	if got := outcomeOf(t, phone.sync(t), "main").Outcome; got != liveops.OutcomeUploaded {
		t.Fatalf("initial upload = %v", got)
	}
	if got := outcomeOf(t, tablet.sync(t), "main").Outcome; got != liveops.OutcomeDownloaded {
		t.Fatalf("initial download = %v", got)
	}

	// Step 1: Phone updates to 1.3 (save version 3), saves, and syncs.
	reconfigureDevice(t, phone, "phone", cloud, 3, nil)
	phone.save(t, "main", progress{Level: 10, Note: "phone-v3"}, time.Hour)
	if got := outcomeOf(t, phone.sync(t), "main").Outcome; got != liveops.OutcomeUploaded {
		t.Fatalf("phone v3 upload = %v", got)
	}
	if v, p := cloudProgress(t, cloud, "main"); v.EnvelopeVersion != 3 || p.Level != 10 {
		t.Fatalf("cloud after phone upload = env v%d %+v", v.EnvelopeVersion, p)
	}

	// Step 2: Tablet (still on v2) syncs: must not overwrite its local v2 copy with v3, and must report OutcomeNeedsUpdate with both versions.
	res := tablet.sync(t)
	r := outcomeOf(t, res, "main")
	if r.Outcome != liveops.OutcomeNeedsUpdate || r.Outcome.String() != "needs-update" {
		t.Fatalf("tablet sync = %+v, want OutcomeNeedsUpdate", r)
	}
	if r.LocalVersion != 2 || r.CloudVersion != 3 || r.Conflict != nil {
		t.Fatalf("tablet versions = local %d cloud %d conflict %+v, want 2, 3, nil", r.LocalVersion, r.CloudVersion, r.Conflict)
	}
	if nu := res.NeedsUpdate(); len(nu) != 1 || nu[0].Slot != "main" {
		t.Fatalf("NeedsUpdate() = %+v", nu)
	}

	// Step 3: Store.Load on the tablet still loads its v2 save cleanly (never ErrFutureVersion).
	if got := tablet.load(t, "main"); got.Level != 4 || got.Note != "shared-v2" {
		t.Fatalf("tablet local save after needs-update = %+v", got)
	}

	// Step 4: Even if the tablet starts fresh (or plays on) and saves a newer-timestamp v2 save,
	// the next sync must never upload over the cloud's v3 save, and must keep both copies safely.
	tablet.save(t, "main", progress{Level: 1, Note: "tablet-fresh-v2"}, 2*time.Hour)
	r = outcomeOf(t, tablet.sync(t), "main")
	if r.Outcome != liveops.OutcomeNeedsUpdate || r.LocalVersion != 2 || r.CloudVersion != 3 {
		t.Fatalf("tablet post-fresh sync = %+v", r)
	}
	if r.Conflict == nil || r.Conflict.Local.EnvelopeVersion != 2 || r.Conflict.Cloud.EnvelopeVersion != 3 {
		t.Fatalf("conflict versions = %+v", r.Conflict)
	}
	if r.Fork != "main.conflict-tablet" {
		t.Fatalf("fork = %q, want main.conflict-tablet", r.Fork)
	}
	// Cloud "main" is still the phone's v3 save.
	if v, p := cloudProgress(t, cloud, "main"); v.EnvelopeVersion != 3 || p.Level != 10 || p.Note != "phone-v3" {
		t.Fatalf("cloud main was clobbered: env v%d %+v", v.EnvelopeVersion, p)
	}
	// Cloud fork holds the tablet's v2 save.
	if v, p := cloudProgress(t, cloud, r.Fork); v.EnvelopeVersion != 2 || p.Level != 1 || p.Note != "tablet-fresh-v2" {
		t.Fatalf("cloud fork = env v%d %+v", v.EnvelopeVersion, p)
	}
	// Tablet's local "main" is still its loadable v2 save (not overwritten with v3).
	if got := tablet.load(t, "main"); got.Level != 1 || got.Note != "tablet-fresh-v2" {
		t.Fatalf("tablet local main = %+v", got)
	}

	// Repeated sync on tablet reuses the fork without creating "-2" and still protects cloud v3.
	r = outcomeOf(t, tablet.sync(t), "main")
	if r.Outcome != liveops.OutcomeNeedsUpdate || r.Fork != "main.conflict-tablet" {
		t.Fatalf("repeat tablet sync = %+v", r)
	}
	if _, ok := cloud.slots["main.conflict-tablet-2"]; ok {
		t.Fatal("repeat sync created duplicate fork")
	}

	// Phone sees untouched v3 in main and downloads the tablet's v2 fork (migrated on load).
	phoneRes := phone.sync(t)
	if got := outcomeOf(t, phoneRes, "main").Outcome; got != liveops.OutcomeUpToDate {
		t.Fatalf("phone main = %v", got)
	}
	if got := outcomeOf(t, phoneRes, "main.conflict-tablet").Outcome; got != liveops.OutcomeDownloaded {
		t.Fatalf("phone fork = %v", got)
	}
	if got := phone.load(t, "main.conflict-tablet"); got.Level != 1 || got.Note != "tablet-fresh-v2" {
		t.Fatalf("phone loaded tablet fork = %+v", got)
	}

	// Once the tablet updates to 1.3 (v3), syncing converges cleanly: main becomes v3 and both slots settle.
	reconfigureDevice(t, tablet, "tablet", cloud, 3, nil)
	r = outcomeOf(t, tablet.sync(t), "main")
	if r.Outcome != liveops.OutcomeKeptBoth || r.Fork != "main.conflict-tablet" {
		t.Fatalf("tablet upgraded sync = %+v", r)
	}
	if got := tablet.load(t, "main"); got.Level != 10 || got.Note != "phone-v3" {
		t.Fatalf("tablet upgraded main = %+v", got)
	}
	if got := tablet.load(t, "main.conflict-tablet"); got.Level != 1 || got.Note != "tablet-fresh-v2" {
		t.Fatalf("tablet upgraded fork = %+v", got)
	}
	for _, sr := range tablet.sync(t).Slots {
		if sr.Outcome != liveops.OutcomeUpToDate {
			t.Fatalf("tablet not up to date after upgrade: %+v", sr)
		}
	}
}

func TestCloudSyncNewerVersionEdgeCases(t *testing.T) {
	t.Run("fresh older device does not write newer cloud save while syncing compatible slots", func(t *testing.T) {
		cloud := newMemCloud()
		phone := newVersionedDevice(t, "phone", cloud, 3, nil)
		phone.save(t, "future", progress{Level: 12, Note: "v3"}, time.Hour)
		phone.sync(t)

		// Also place a v2 slot and a v3 envelope with an unknown future field in the cloud.
		v2Raw, _ := json.Marshal(map[string]any{"version": 2, "data": progress{Level: 2, Note: "v2"}})
		cloud.slots["compat"] = v2Raw
		cloud.slots["extra"] = []byte(`{"version":3,"checksum":"abc","data":{"level":9,"note":"extra"}}`)

		tablet := newVersionedDevice(t, "tablet", cloud, 2, nil)
		res := tablet.sync(t)

		if r := outcomeOf(t, res, "compat"); r.Outcome != liveops.OutcomeDownloaded {
			t.Fatalf("compat slot = %+v", r)
		}
		if got := tablet.load(t, "compat"); got.Level != 2 {
			t.Fatalf("compat loaded = %+v", got)
		}
		for _, slot := range []string{"future", "extra"} {
			r := outcomeOf(t, res, slot)
			if r.Outcome != liveops.OutcomeNeedsUpdate || r.LocalVersion != 2 || r.CloudVersion != 3 {
				t.Fatalf("%s slot = %+v, want needs-update (2 vs 3)", slot, r)
			}
			if tablet.store.Exists(slot) {
				t.Fatalf("%s was written locally on older client", slot)
			}
		}
		if len(res.NeedsUpdate()) != 2 {
			t.Fatalf("NeedsUpdate() = %+v, want 2 slots", res.NeedsUpdate())
		}
	})

	t.Run("downgraded client cannot overwrite or delete newer cloud save", func(t *testing.T) {
		cloud := newMemCloud()
		dev := newVersionedDevice(t, "tablet", cloud, 3, nil)
		dev.save(t, "main", progress{Level: 20, Note: "v3"}, time.Hour)
		dev.save(t, "del", progress{Level: 21, Note: "v3-del"}, time.Hour)
		dev.sync(t)

		// Device downgrades to v2; sync state still points at v3 hash (ch == st.Hash).
		reconfigureDevice(t, dev, "tablet", cloud, 2, nil)
		dev.save(t, "main", progress{Level: 1, Note: "fresh-v2"}, 2*time.Hour)
		if err := dev.store.Delete("del"); err != nil {
			t.Fatal(err)
		}

		res := dev.sync(t)
		rm := outcomeOf(t, res, "main")
		if rm.Outcome != liveops.OutcomeNeedsUpdate || rm.Fork != "main.conflict-tablet" {
			t.Fatalf("downgraded main = %+v", rm)
		}
		if v, p := cloudProgress(t, cloud, "main"); v.EnvelopeVersion != 3 || p.Level != 20 {
			t.Fatalf("downgraded client overwrote cloud main: v%d %+v", v.EnvelopeVersion, p)
		}
		rd := outcomeOf(t, res, "del")
		if rd.Outcome != liveops.OutcomeNeedsUpdate || rd.LocalVersion != 2 || rd.CloudVersion != 3 {
			t.Fatalf("downgraded del = %+v", rd)
		}
		if v, p := cloudProgress(t, cloud, "del"); v.EnvelopeVersion != 3 || p.Level != 21 {
			t.Fatalf("downgraded client deleted cloud del: v%d %+v", v.EnvelopeVersion, p)
		}
	})
}

func TestCloudSyncMixedVersionConflicts(t *testing.T) {
	t.Run("PolicyNewest never resolves in favour of older local envelope", func(t *testing.T) {
		cloud := newMemCloud()
		phone := newVersionedDevice(t, "phone", cloud, 2, nil)
		laptop := newVersionedDevice(t, "laptop", cloud, 2, nil)
		phone.save(t, "main", progress{Level: 1, Note: "base"}, 0)
		phone.sync(t)
		laptop.sync(t)

		// Phone upgrades to v3 and saves at +1h; laptop writes v2 locally at +2h, then upgrades to v3 before syncing (so local is still unmigrated v2, cloud is v3).
		reconfigureDevice(t, phone, "phone", cloud, 3, nil)
		phone.save(t, "main", progress{Level: 5, Note: "phone-v3"}, time.Hour)
		phone.sync(t)
		laptop.save(t, "main", progress{Level: 7, Note: "laptop-v2"}, 2*time.Hour)
		reconfigureDevice(t, laptop, "laptop", cloud, 3, nil)

		r := outcomeOf(t, laptop.sync(t), "main")
		if r.Outcome != liveops.OutcomeKeptBoth || r.Fork != "main.conflict-laptop" || r.Conflict == nil {
			t.Fatalf("laptop sync = %+v, want OutcomeKeptBoth", r)
		}
		if r.Conflict.Local.EnvelopeVersion != 2 || r.Conflict.Cloud.EnvelopeVersion != 3 {
			t.Fatalf("conflict envelope versions = local %d cloud %d", r.Conflict.Local.EnvelopeVersion, r.Conflict.Cloud.EnvelopeVersion)
		}
		if v, p := cloudProgress(t, cloud, "main"); v.EnvelopeVersion != 3 || p.Level != 5 {
			t.Fatalf("cloud main = v%d %+v", v.EnvelopeVersion, p)
		}
		if v, p := cloudProgress(t, cloud, r.Fork); v.EnvelopeVersion != 2 || p.Level != 7 {
			t.Fatalf("cloud fork = v%d %+v", v.EnvelopeVersion, p)
		}
		if got := laptop.load(t, "main"); got.Level != 5 {
			t.Fatalf("laptop main = %+v", got)
		}
		if got := laptop.load(t, r.Fork); got.Level != 7 {
			t.Fatalf("laptop fork = %+v", got)
		}
	})

	t.Run("PolicyNewest never resolves in favour of older cloud envelope", func(t *testing.T) {
		cloud := newMemCloud()
		phone := newVersionedDevice(t, "phone", cloud, 2, nil)
		tablet := newVersionedDevice(t, "tablet", cloud, 2, nil)
		phone.save(t, "main", progress{Level: 1, Note: "base"}, 0)
		phone.sync(t)
		tablet.sync(t)

		// Phone upgrades to v3 and saves offline at +1h; tablet stays on v2, saves at +2h and uploads.
		reconfigureDevice(t, phone, "phone", cloud, 3, nil)
		phone.save(t, "main", progress{Level: 8, Note: "phone-v3"}, time.Hour)
		tablet.save(t, "main", progress{Level: 6, Note: "tablet-v2"}, 2*time.Hour)
		tablet.sync(t)

		// Phone syncs: cloud has newer timestamp (+2h) but older envelope version (v2 < v3), so keep both.
		r := outcomeOf(t, phone.sync(t), "main")
		if r.Outcome != liveops.OutcomeKeptBoth || r.Fork != "main.conflict-phone" || r.Conflict == nil {
			t.Fatalf("phone sync = %+v, want OutcomeKeptBoth", r)
		}
		if r.Conflict.Local.EnvelopeVersion != 3 || r.Conflict.Cloud.EnvelopeVersion != 2 {
			t.Fatalf("conflict envelope versions = local %d cloud %d", r.Conflict.Local.EnvelopeVersion, r.Conflict.Cloud.EnvelopeVersion)
		}
		if v, p := cloudProgress(t, cloud, r.Fork); v.EnvelopeVersion != 3 || p.Level != 8 {
			t.Fatalf("phone v3 fork = v%d %+v", v.EnvelopeVersion, p)
		}
		if got := phone.load(t, "main"); got.Level != 6 {
			t.Fatalf("phone main (migrated v2) = %+v", got)
		}
		if got := phone.load(t, r.Fork); got.Level != 8 {
			t.Fatalf("phone fork (v3) = %+v", got)
		}
	})

	t.Run("PolicyNewest lets newer version with newer time win", func(t *testing.T) {
		cloud := newMemCloud()
		phone := newVersionedDevice(t, "phone", cloud, 2, nil)
		tablet := newVersionedDevice(t, "tablet", cloud, 2, nil)
		phone.save(t, "main", progress{Level: 1, Note: "base"}, 0)
		phone.sync(t)
		tablet.sync(t)

		tablet.save(t, "main", progress{Level: 3, Note: "tablet-v2"}, time.Hour)
		tablet.sync(t)
		reconfigureDevice(t, phone, "phone", cloud, 3, nil)
		phone.save(t, "main", progress{Level: 9, Note: "phone-v3"}, 2*time.Hour)

		r := outcomeOf(t, phone.sync(t), "main")
		if r.Outcome != liveops.OutcomeKeptLocal || r.Discarded == nil || r.Discarded.EnvelopeVersion != 2 {
			t.Fatalf("phone sync = %+v, want OutcomeKeptLocal", r)
		}
		if v, p := cloudProgress(t, cloud, "main"); v.EnvelopeVersion != 3 || p.Level != 9 {
			t.Fatalf("cloud main = v%d %+v", v.EnvelopeVersion, p)
		}
	})

	t.Run("PolicyNewest on older client with newer cloud timestamp keeps local and reports needs-update", func(t *testing.T) {
		cloud := newMemCloud()
		phone := newVersionedDevice(t, "phone", cloud, 2, nil)
		tablet := newVersionedDevice(t, "tablet", cloud, 2, nil)
		phone.save(t, "main", progress{Level: 1, Note: "base"}, 0)
		phone.sync(t)
		tablet.sync(t)

		tablet.save(t, "main", progress{Level: 4, Note: "tablet-v2"}, time.Hour)
		reconfigureDevice(t, phone, "phone", cloud, 3, nil)
		phone.save(t, "main", progress{Level: 9, Note: "phone-v3"}, 2*time.Hour)
		phone.sync(t)

		r := outcomeOf(t, tablet.sync(t), "main")
		if r.Outcome != liveops.OutcomeNeedsUpdate || r.LocalVersion != 2 || r.CloudVersion != 3 || r.Conflict == nil {
			t.Fatalf("tablet sync = %+v", r)
		}
		if r.Conflict.Local.EnvelopeVersion != 2 || r.Conflict.Cloud.EnvelopeVersion != 3 {
			t.Fatalf("conflict = %+v", r.Conflict)
		}
		if got := tablet.load(t, "main"); got.Level != 4 {
			t.Fatalf("tablet local overwritten: %+v", got)
		}
		if v, p := cloudProgress(t, cloud, "main"); v.EnvelopeVersion != 3 || p.Level != 9 {
			t.Fatalf("cloud main overwritten: v%d %+v", v.EnvelopeVersion, p)
		}
	})

	t.Run("PolicyAsk reports envelope versions and Resolve guards mixed versions", func(t *testing.T) {
		cloud := newMemCloud()
		phone := newVersionedDevice(t, "phone", cloud, 2, nil)
		var pending []liveops.Conflict
		tablet := newVersionedDevice(t, "tablet", cloud, 2, func(o *liveops.CloudSyncOptions) {
			o.Policy = liveops.PolicyAsk
			o.Ask = func(_ context.Context, c liveops.Conflict) (liveops.Choice, error) {
				pending = append(pending, c)
				return liveops.ChoiceDefer, nil
			}
		})
		phone.save(t, "main", progress{Level: 1, Note: "base"}, 0)
		phone.sync(t)
		tablet.sync(t)

		reconfigureDevice(t, phone, "phone", cloud, 3, nil)
		phone.save(t, "main", progress{Level: 10, Note: "phone-v3"}, time.Hour)
		phone.sync(t)
		tablet.save(t, "main", progress{Level: 5, Note: "tablet-v2"}, 2*time.Hour)

		r := outcomeOf(t, tablet.sync(t), "main")
		if r.Outcome != liveops.OutcomeDeferred || len(pending) != 1 {
			t.Fatalf("tablet ask sync = %+v, pending=%d", r, len(pending))
		}
		c := pending[0]
		if c.Local.EnvelopeVersion != 2 || c.Cloud.EnvelopeVersion != 3 {
			t.Fatalf("Ask conflict versions = local %d cloud %d, want 2 and 3", c.Local.EnvelopeVersion, c.Cloud.EnvelopeVersion)
		}

		// Choosing Cloud on older client refuses to write v3 locally and reports OutcomeNeedsUpdate.
		ctx := context.Background()
		rc, err := tablet.syncer.Resolve(ctx, c, liveops.ChoiceCloud)
		if err != nil || rc.Outcome != liveops.OutcomeNeedsUpdate || rc.LocalVersion != 2 || rc.CloudVersion != 3 {
			t.Fatalf("Resolve(ChoiceCloud) = %+v, %v", rc, err)
		}
		if got := tablet.load(t, "main"); got.Level != 5 {
			t.Fatalf("local overwritten after Resolve(ChoiceCloud): %+v", got)
		}

		// Choosing Local refuses to overwrite cloud v3; forks local v2 and reports OutcomeNeedsUpdate.
		rl, err := tablet.syncer.Resolve(ctx, c, liveops.ChoiceLocal)
		if err != nil || rl.Outcome != liveops.OutcomeNeedsUpdate || rl.Fork != "main.conflict-tablet" {
			t.Fatalf("Resolve(ChoiceLocal) = %+v, %v", rl, err)
		}
		if v, p := cloudProgress(t, cloud, "main"); v.EnvelopeVersion != 3 || p.Level != 10 {
			t.Fatalf("cloud v3 overwritten by Resolve(ChoiceLocal): v%d %+v", v.EnvelopeVersion, p)
		}
		if v, p := cloudProgress(t, cloud, rl.Fork); v.EnvelopeVersion != 2 || p.Level != 5 {
			t.Fatalf("fork after Resolve(ChoiceLocal) = v%d %+v", v.EnvelopeVersion, p)
		}
	})
}
