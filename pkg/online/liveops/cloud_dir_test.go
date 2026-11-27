package liveops_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/liveops"
	"github.com/nsgo-dev/game/pkg/save"
)

// Two desktop machines sharing a Steam Auto-Cloud directory sync saves, deletions and conflicts through DirCloud and CloudSyncer.
func TestDirCloudSteamAutoCloudTwoMachines(t *testing.T) {
	steamCloudDir := filepath.Join(t.TempDir(), "steam-cloud")

	cloudPC, err := liveops.NewDirCloud(steamCloudDir)
	if err != nil {
		t.Fatal(err)
	}
	cloudDeck, err := liveops.NewDirCloud(steamCloudDir)
	if err != nil {
		t.Fatal(err)
	}
	if cloudPC.Dir() != steamCloudDir {
		t.Fatalf("Dir() = %q, want %q", cloudPC.Dir(), steamCloudDir)
	}

	pc := newDevice(t, "steam-pc", cloudPC, func(o *liveops.CloudSyncOptions) {
		o.Policy = liveops.PolicyKeepBoth
	})
	deck := newDevice(t, "steam-deck", cloudDeck, func(o *liveops.CloudSyncOptions) {
		o.Policy = liveops.PolicyKeepBoth
	})

	// PC saves slot "main" and syncs; Deck downloads it.
	pc.save(t, "main", progress{Level: 12, Note: "desktop"}, 0)
	if got := outcomeOf(t, pc.sync(t), "main").Outcome; got != liveops.OutcomeUploaded {
		t.Fatalf("pc upload = %v", got)
	}
	if got := outcomeOf(t, deck.sync(t), "main").Outcome; got != liveops.OutcomeDownloaded {
		t.Fatalf("deck download = %v", got)
	}
	if got := deck.load(t, "main"); got != (progress{Level: 12, Note: "desktop"}) {
		t.Fatalf("deck loaded = %+v", got)
	}

	// Both devices edit offline: PC syncs first, Deck keeps both copies via fork slot.
	pc.save(t, "main", progress{Level: 15, Note: "pc-branch"}, time.Hour)
	deck.save(t, "main", progress{Level: 14, Note: "deck-branch"}, 2*time.Hour)
	if got := outcomeOf(t, pc.sync(t), "main").Outcome; got != liveops.OutcomeUploaded {
		t.Fatalf("pc second upload = %v", got)
	}
	deckRes := outcomeOf(t, deck.sync(t), "main")
	if deckRes.Outcome != liveops.OutcomeKeptBoth || deckRes.Fork == "" {
		t.Fatalf("deck conflict outcome = %+v", deckRes)
	}
	if got := deck.load(t, "main"); got.Level != 15 {
		t.Fatalf("deck main = %+v, want level 15", got)
	}
	if got := deck.load(t, deckRes.Fork); got.Level != 14 {
		t.Fatalf("deck fork = %+v, want level 14", got)
	}

	// PC pulls the fork slot too.
	if got := outcomeOf(t, pc.sync(t), deckRes.Fork).Outcome; got != liveops.OutcomeDownloaded {
		t.Fatalf("pc fork download = %v", got)
	}
	if got := pc.load(t, deckRes.Fork); got.Level != 14 {
		t.Fatalf("pc fork = %+v, want level 14", got)
	}

	// Deletion propagates from PC to Deck.
	if err := pc.store.Delete(deckRes.Fork); err != nil {
		t.Fatal(err)
	}
	if got := outcomeOf(t, pc.sync(t), deckRes.Fork).Outcome; got != liveops.OutcomeDeletedCloud {
		t.Fatalf("pc delete fork = %v", got)
	}
	if got := outcomeOf(t, deck.sync(t), deckRes.Fork).Outcome; got != liveops.OutcomeDeletedLocal {
		t.Fatalf("deck delete fork = %v", got)
	}

	// No stray staging temp files remain in the synced directory.
	entries, err := os.ReadDir(steamCloudDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover temp file in cloud dir: %s", e.Name())
		}
	}
}

func TestDirCloudConditionalWrites(t *testing.T) {
	c, err := liveops.NewDirCloud(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Empty-match DeleteIf on absent slot succeeds.
	if err := c.DeleteIf(ctx, "main", ""); err != nil {
		t.Fatalf("DeleteIf absent with empty match: %v", err)
	}
	// Non-empty-match DeleteIf on absent slot fails with ErrCloudChanged.
	if err := c.DeleteIf(ctx, "main", liveops.PayloadHash([]byte("v1"))); !errors.Is(err, liveops.ErrCloudChanged) {
		t.Fatalf("DeleteIf absent with hash = %v, want ErrCloudChanged", err)
	}
	// Non-empty-match SaveIf on absent slot fails with ErrCloudChanged.
	if err := c.SaveIf(ctx, "main", []byte("v1"), liveops.PayloadHash([]byte("other"))); !errors.Is(err, liveops.ErrCloudChanged) {
		t.Fatalf("SaveIf absent with hash = %v, want ErrCloudChanged", err)
	}

	// Create with empty match succeeds.
	v1 := []byte(`{"v":1}`)
	if err := c.SaveIf(ctx, "main", v1, ""); err != nil {
		t.Fatalf("SaveIf create: %v", err)
	}
	// Create again with empty match fails with ErrCloudChanged.
	if err := c.SaveIf(ctx, "main", []byte(`{"v":2}`), ""); !errors.Is(err, liveops.ErrCloudChanged) {
		t.Fatalf("SaveIf duplicate create = %v, want ErrCloudChanged", err)
	}

	// Conditional update with matching hash succeeds; stale hash fails.
	v2 := []byte(`{"v":2}`)
	if err := c.SaveIf(ctx, "main", v2, liveops.PayloadHash(v1)); err != nil {
		t.Fatalf("SaveIf matching hash: %v", err)
	}
	if err := c.SaveIf(ctx, "main", []byte(`{"v":3}`), liveops.PayloadHash(v1)); !errors.Is(err, liveops.ErrCloudChanged) {
		t.Fatalf("SaveIf stale hash = %v, want ErrCloudChanged", err)
	}

	// DeleteIf with stale hash leaves v2 intact; matching hash deletes.
	if err := c.DeleteIf(ctx, "main", liveops.PayloadHash(v1)); !errors.Is(err, liveops.ErrCloudChanged) {
		t.Fatalf("DeleteIf stale hash = %v, want ErrCloudChanged", err)
	}
	got, err := c.Load(ctx, "main")
	if err != nil || string(got) != string(v2) {
		t.Fatalf("Load after failed DeleteIf = %q, %v", got, err)
	}
	if err := c.DeleteIf(ctx, "main", liveops.PayloadHash(v2)); err != nil {
		t.Fatalf("DeleteIf matching hash: %v", err)
	}
	if _, err := c.Load(ctx, "main"); !errors.Is(err, liveops.ErrSlotNotFound) {
		t.Fatalf("Load after DeleteIf = %v, want ErrSlotNotFound", err)
	}
}

func TestDirCloudValidationFilteringAndSweep(t *testing.T) {
	for _, empty := range []string{"", "   "} {
		if _, err := liveops.NewDirCloud(empty); !errors.Is(err, liveops.ErrCloudUnavailable) {
			t.Errorf("NewDirCloud(%q) = %v, want ErrCloudUnavailable", empty, err)
		}
	}

	dir := filepath.Join(t.TempDir(), "not-yet-created")
	c, err := liveops.NewDirCloud(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// List on missing directory returns nil, nil.
	if slots, err := c.List(ctx); err != nil || len(slots) != 0 {
		t.Fatalf("List missing dir = %v, %v", slots, err)
	}

	// Pre-create directory with stray files and a stale temp file.
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleTmp := filepath.Join(dir, ".cloud-old.tmp")
	if err := os.WriteFile(staleTmp, []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(staleTmp, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// First Save creates/writes slot and sweeps stale temp file.
	if err := c.Save(ctx, "Autosave", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staleTmp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale temp file not swept: %v", err)
	}

	// Case-folding collision is rejected.
	if err := c.Save(ctx, "autosave", []byte(`{"collide":true}`)); !errors.Is(err, save.ErrInvalidSlot) {
		t.Fatalf("case-folding Save = %v, want save.ErrInvalidSlot", err)
	}

	// List returns only valid save slots.
	slots, err := c.List(ctx)
	if err != nil || !slices.Equal(slots, []string{"Autosave"}) {
		t.Fatalf("List = %v, %v; want [Autosave]", slots, err)
	}

	// Invalid slots and cancelled contexts are rejected.
	if _, err := c.Load(ctx, "../escape"); !errors.Is(err, save.ErrInvalidSlot) {
		t.Fatalf("Load traversal = %v, want save.ErrInvalidSlot", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.List(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("List(cancelled) = %v, want context.Canceled", err)
	}
	if err := c.Save(cancelled, "slot1", []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save(cancelled) = %v, want context.Canceled", err)
	}
	if err := c.Delete(cancelled, "Autosave"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete(cancelled) = %v, want context.Canceled", err)
	}
}
