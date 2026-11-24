package modding_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/modding"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

func copySampleBossRushMod(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "boss-rush")
	copyFile(t, filepath.Join("testdata", "mods", "boss-rush", "mod.yaml"), filepath.Join(dir, "mod.yaml"))
	copyFile(t, filepath.Join("testdata", "mods", "boss-rush", "mod.tsx"), filepath.Join(dir, "mod.tsx"))
	if err := os.WriteFile(filepath.Join(dir, "preview.png"), []byte("fake-png-preview"), 0o644); err != nil {
		t.Fatalf("write preview: %v", err)
	}
	return dir
}

func TestWorkshopSampleModPublishAndSubscribedInstall(t *testing.T) {
	ctx := context.Background()
	hub := modding.NewMemoryWorkshop(t.TempDir())
	authorDir := copySampleBossRushMod(t)

	authorWS := modding.NewWorkshop(hub.AsUser(1001), nil, modding.WorkshopOptions{AppID: 480})
	pubRes, err := authorWS.Publish(ctx, authorDir, modding.PublishOptions{
		Tags:       []string{"gameplay", "bosses"},
		Visibility: modding.VisibilityPublic,
		Preview:    "preview.png",
		ChangeNote: "Initial Workshop release",
	})
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}
	if pubRes.ID == 0 {
		t.Fatal("expected non-zero PublishedFileID")
	}
	if pubRes.NeedsLegalAgreement {
		t.Fatal("unexpected NeedsLegalAgreement")
	}

	// Verify workshop.yaml sidecar was written deterministically and round-trips identically.
	wsBytes, err := os.ReadFile(filepath.Join(authorDir, modding.WorkshopFile))
	if err != nil {
		t.Fatalf("read workshop.yaml: %v", err)
	}
	meta, err := modding.ParseWorkshopMetadata(wsBytes)
	if err != nil {
		t.Fatalf("ParseWorkshopMetadata: %v", err)
	}
	if meta.AppID != 480 || meta.PublishedFileID != pubRes.ID || meta.Title != "Boss Rush" {
		t.Fatalf("unexpected workshop metadata: %+v", meta)
	}
	if !slices.Equal(meta.Tags, []string{"bosses", "gameplay"}) {
		t.Fatalf("tags = %v, want [bosses gameplay]", meta.Tags)
	}
	if meta.Visibility != modding.VisibilityPublic || meta.Preview != "preview.png" {
		t.Fatalf("unexpected visibility/preview: %+v", meta)
	}
	reencoded, err := modding.EncodeWorkshopMetadata(meta)
	if err != nil {
		t.Fatalf("EncodeWorkshopMetadata: %v", err)
	}
	if !bytes.Equal(wsBytes, reencoded) {
		t.Fatalf("workshop.yaml is not canonical:\ngot:\n%s\nwant:\n%s", wsBytes, reencoded)
	}

	// Another player discovers the item on Workshop, subscribes, and installs it through Manager.
	var (
		mgr     *modding.Manager
		spawns  []modding.SpawnRequest
		emitted []modding.Event
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{
			GameVersion: "0.2.0",
			Bridge: modding.GameBridge{
				OnSpawn: func(req modding.SpawnRequest) (string, error) {
					spawns = append(spawns, req)
					return "ent-1", nil
				},
				OnEmit: func(ev modding.Event) {
					emitted = append(emitted, ev)
				},
			},
		})
		return core.View{}
	})

	playerWS := modding.NewWorkshop(hub.AsUser(2002), mgr, modding.WorkshopOptions{
		AppID:    480,
		AutoLoad: true,
	})

	found, err := playerWS.QueryItems(ctx, modding.WorkshopQuery{
		AppID:      480,
		SearchText: "boss rush",
		Tags:       []string{"bosses"},
	})
	if err != nil {
		t.Fatalf("QueryItems failed: %v", err)
	}
	if len(found) != 1 || found[0].ID != pubRes.ID {
		t.Fatalf("QueryItems = %+v, want item %s", found, pubRes.ID)
	}

	if err := playerWS.Subscribe(ctx, pubRes.ID); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	var syncRes modding.SyncResult
	app.Post(func() {
		syncRes, err = playerWS.SyncSubscribed(ctx)
	})
	app.Settle()
	if err != nil {
		t.Fatalf("SyncSubscribed failed: %v", err)
	}
	if len(syncRes.Installed) != 1 || syncRes.Installed[0].ModID != "com.example.boss-rush" {
		t.Fatalf("Installed = %+v, want com.example.boss-rush", syncRes.Installed)
	}

	info, ok := mgr.Info("com.example.boss-rush")
	if !ok || info.State != modding.StateLoaded {
		t.Fatalf("mod info = (%+v, %v), want StateLoaded", info, ok)
	}

	app.Post(func() {
		mgr.Step(5 * time.Second)
	})
	app.Settle()
	if len(spawns) != 1 || spawns[0].Prefab != "slime" {
		t.Fatalf("spawns = %+v, want 1 slime spawn", spawns)
	}
	if len(emitted) != 1 || emitted[0].Name != "boss-rush:wave" {
		t.Fatalf("emitted = %+v, want boss-rush:wave", emitted)
	}
	if panels := mgr.Panels("hud"); len(panels) != 1 || panels[0].ID != "boss-timer" {
		t.Fatalf("hud panels = %+v, want boss-timer", panels)
	}

	// Author updates the mod and re-publishes; publishedFileId in workshop.yaml is reused.
	updatedManifest := strings.Replace(mustReadString(t, filepath.Join(authorDir, "mod.yaml")), "version: 1.2.0", "version: 1.3.0", 1)
	if err := os.WriteFile(filepath.Join(authorDir, "mod.yaml"), []byte(updatedManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	updatedScript := strings.Replace(mustReadString(t, filepath.Join(authorDir, "mod.tsx")), `"slime"`, `"mega-slime"`, 1)
	if err := os.WriteFile(filepath.Join(authorDir, "mod.tsx"), []byte(updatedScript), 0o644); err != nil {
		t.Fatal(err)
	}

	pubRes2, err := authorWS.Publish(ctx, authorDir, modding.PublishOptions{
		ChangeNote: "Upgrade slime to mega-slime",
	})
	if err != nil {
		t.Fatalf("second Publish failed: %v", err)
	}
	if pubRes2.ID != pubRes.ID {
		t.Fatalf("second Publish allocated new ID %s, want %s", pubRes2.ID, pubRes.ID)
	}
	wsBytes2, err := os.ReadFile(filepath.Join(authorDir, modding.WorkshopFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wsBytes, wsBytes2) {
		t.Fatalf("workshop.yaml changed on re-publish:\nbefore:\n%s\nafter:\n%s", wsBytes, wsBytes2)
	}

	// Subscribed player syncs again -> downloads update and reloads mod with new version.
	app.Post(func() {
		syncRes, err = playerWS.SyncSubscribed(ctx)
	})
	app.Settle()
	if err != nil {
		t.Fatalf("SyncSubscribed (update) failed: %v", err)
	}
	if len(syncRes.Updated) != 1 || syncRes.Updated[0].Manifest.Version != "1.3.0" {
		t.Fatalf("Updated = %+v, want version 1.3.0", syncRes.Updated)
	}

	app.Post(func() {
		mgr.Step(5 * time.Second)
	})
	app.Settle()
	if len(spawns) != 2 || spawns[1].Prefab != "mega-slime" {
		t.Fatalf("spawns after update = %+v, want second spawn mega-slime", spawns)
	}

	// Syncing when nothing changed reports the item as Unchanged.
	app.Post(func() {
		syncRes, err = playerWS.SyncSubscribed(ctx)
	})
	app.Settle()
	if err != nil {
		t.Fatalf("SyncSubscribed (unchanged) failed: %v", err)
	}
	if len(syncRes.Unchanged) != 1 || len(syncRes.Installed) != 0 || len(syncRes.Updated) != 0 {
		t.Fatalf("unexpected unchanged sync result: %+v", syncRes)
	}

	// Unsubscribing removes the mod from Manager and cleans up its HUD panels.
	if err := playerWS.Unsubscribe(ctx, pubRes.ID); err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}
	app.Post(func() {
		syncRes, err = playerWS.SyncSubscribed(ctx)
	})
	app.Settle()
	if err != nil {
		t.Fatalf("SyncSubscribed (unsubscribe) failed: %v", err)
	}
	if len(syncRes.Removed) != 1 || syncRes.Removed[0].ID != pubRes.ID {
		t.Fatalf("Removed = %+v, want %s", syncRes.Removed, pubRes.ID)
	}
	if _, exists := mgr.Info("com.example.boss-rush"); exists {
		t.Fatal("expected unsubscribed mod to be removed from Manager")
	}
	if panels := mgr.Panels("hud"); len(panels) != 0 {
		t.Fatalf("expected HUD panels removed after unsubscribe, got %+v", panels)
	}
	if got := playerWS.Installed(); len(got) != 0 {
		t.Fatalf("Installed() = %+v, want empty", got)
	}
}

func TestWorkshopPublishRefusesInvalidManifestAndUnsafePaths(t *testing.T) {
	ctx := context.Background()
	hub := modding.NewMemoryWorkshop(t.TempDir())
	ws := modding.NewWorkshop(hub, nil, modding.WorkshopOptions{AppID: 480})

	cases := []struct {
		name    string
		setup   func(t *testing.T) string
		opts    modding.PublishOptions
		wantErr error
	}{
		{
			name: "missing mod.yaml",
			setup: func(t *testing.T) string {
				d := t.TempDir()
				_ = os.WriteFile(filepath.Join(d, "mod.ts"), []byte("export {};\n"), 0o644)
				return d
			},
			wantErr: modding.ErrInvalidManifest,
		},
		{
			name: "invalid manifest unknown field",
			setup: func(t *testing.T) string {
				d := t.TempDir()
				_ = os.WriteFile(filepath.Join(d, "mod.yaml"), []byte("manifestVersion: 1\nid: bad.mod\nname: Bad\nversion: 1.0.0\nentry: mod.ts\nextra: true\n"), 0o644)
				_ = os.WriteFile(filepath.Join(d, "mod.ts"), []byte("export {};\n"), 0o644)
				return d
			},
			wantErr: modding.ErrInvalidManifest,
		},
		{
			name: "missing entry script",
			setup: func(t *testing.T) string {
				d := t.TempDir()
				_ = os.WriteFile(filepath.Join(d, "mod.yaml"), []byte("manifestVersion: 1\nid: missing.entry\nname: Missing\nversion: 1.0.0\nentry: missing.ts\n"), 0o644)
				return d
			},
			wantErr: modding.ErrInvalidManifest,
		},
		{
			name: "invalid workshop.yaml unknown field",
			setup: func(t *testing.T) string {
				d := copySampleBossRushMod(t)
				_ = os.WriteFile(filepath.Join(d, modding.WorkshopFile), []byte("version: 1\nappId: 480\nbogus: 123\n"), 0o644)
				return d
			},
			wantErr: modding.ErrInvalidWorkshopMetadata,
		},
		{
			name: "escaping preview path",
			setup: func(t *testing.T) string {
				return copySampleBossRushMod(t)
			},
			opts:    modding.PublishOptions{Preview: "../outside.png"},
			wantErr: modding.ErrInvalidWorkshopMetadata,
		},
		{
			name: "missing preview file",
			setup: func(t *testing.T) string {
				return copySampleBossRushMod(t)
			},
			opts:    modding.PublishOptions{Preview: "does-not-exist.png"},
			wantErr: modding.ErrInvalidWorkshopMetadata,
		},
		{
			name: "invalid visibility",
			setup: func(t *testing.T) string {
				return copySampleBossRushMod(t)
			},
			opts:    modding.PublishOptions{Visibility: "secret"},
			wantErr: modding.ErrInvalidWorkshopMetadata,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.setup(t)
			if _, err := ws.Publish(ctx, dir, tc.opts); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Publish err = %v, want %v", err, tc.wantErr)
			}
		})
	}

	// Zero AppID is refused before creating an item.
	wsNoApp := modding.NewWorkshop(hub, nil, modding.WorkshopOptions{})
	if _, err := wsNoApp.Publish(ctx, copySampleBossRushMod(t), modding.PublishOptions{}); !errors.Is(err, modding.ErrInvalidWorkshopMetadata) {
		t.Fatalf("Publish with zero appId err = %v, want ErrInvalidWorkshopMetadata", err)
	}

	// No Workshop items should have been created by any rejected publish.
	items, err := hub.QueryItems(ctx, modding.WorkshopQuery{})
	if err != nil {
		t.Fatalf("QueryItems failed: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 created Workshop items after invalid publishes, got %+v", items)
	}
}

func TestWorkshopPublishLegalAgreementRequired(t *testing.T) {
	ctx := context.Background()
	hub := modding.NewMemoryWorkshop(t.TempDir())
	hub.SetNeedsLegalAgreement(1001, true)

	dir := copySampleBossRushMod(t)
	ws := modding.NewWorkshop(hub.AsUser(1001), nil, modding.WorkshopOptions{AppID: 480})

	res, err := ws.Publish(ctx, dir, modding.PublishOptions{ChangeNote: "First upload"})
	if !errors.Is(err, modding.ErrWorkshopLegalAgreementRequired) {
		t.Fatalf("Publish err = %v, want ErrWorkshopLegalAgreementRequired", err)
	}
	if res.ID == 0 || !res.NeedsLegalAgreement || res.LegalAgreementURL != modding.WorkshopLegalAgreementURL {
		t.Fatalf("unexpected PublishResult: %+v", res)
	}

	// workshop.yaml still recorded publishedFileId so retrying after accepting updates the same item.
	data, err := os.ReadFile(filepath.Join(dir, modding.WorkshopFile))
	if err != nil {
		t.Fatalf("read workshop.yaml: %v", err)
	}
	meta, err := modding.ParseWorkshopMetadata(data)
	if err != nil {
		t.Fatalf("ParseWorkshopMetadata: %v", err)
	}
	if meta.PublishedFileID != res.ID {
		t.Fatalf("saved publishedFileId = %s, want %s", meta.PublishedFileID, res.ID)
	}

	// After accepting the agreement, re-publishing succeeds and reuses the same ID.
	hub.SetNeedsLegalAgreement(1001, false)
	res2, err := ws.Publish(ctx, dir, modding.PublishOptions{ChangeNote: "Post-agreement upload"})
	if err != nil {
		t.Fatalf("re-publish after agreement failed: %v", err)
	}
	if res2.ID != res.ID || res2.NeedsLegalAgreement {
		t.Fatalf("unexpected PublishResult after agreement: %+v", res2)
	}
}

func TestWorkshopSyncSubscribedDependencyOrderAndFaultIsolation(t *testing.T) {
	ctx := context.Background()
	hub := modding.NewMemoryWorkshop(t.TempDir())
	author := modding.NewWorkshop(hub.AsUser(1001), nil, modding.WorkshopOptions{AppID: 480})

	writeMod := func(name, manifest, script string) string {
		d := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "mod.yaml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "mod.js"), []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		return d
	}

	// Publish dependent mod FIRST (lower PublishedFileID) and base mod SECOND (higher ID),
	// plus an incompatible mod whose gameVersion does not match the player's host.
	addonDir := writeMod("addon-level", `manifestVersion: 1
id: com.example.arena-level
name: Arena Level
version: 1.0.0
entry: mod.js
capabilities: [readState, writeState]
dependencies:
  com.example.level-core: "^1.0.0"
`, `import { getState, setState } from "game";
const seq = getState("order") || [];
seq.push("arena-level");
setState("order", seq);
`)
	baseDir := writeMod("level-core", `manifestVersion: 1
id: com.example.level-core
name: Level Core
version: 1.2.0
entry: mod.js
capabilities: [readState, writeState]
`, `import { getState, setState } from "game";
const seq = getState("order") || [];
seq.push("level-core");
setState("order", seq);
`)
	futureDir := writeMod("future-mod", `manifestVersion: 1
id: com.example.future-mod
name: Future Mod
version: 1.0.0
entry: mod.js
gameVersion: ">=9.0.0"
`, `import { log } from "game"; log("future");`)

	addonPub, err := author.Publish(ctx, addonDir, modding.PublishOptions{Tags: []string{"level"}})
	if err != nil {
		t.Fatalf("publish addon: %v", err)
	}
	basePub, err := author.Publish(ctx, baseDir, modding.PublishOptions{Tags: []string{"library"}})
	if err != nil {
		t.Fatalf("publish base: %v", err)
	}
	futurePub, err := author.Publish(ctx, futureDir, modding.PublishOptions{})
	if err != nil {
		t.Fatalf("publish future: %v", err)
	}

	var mgr *modding.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{GameVersion: "0.2.0"})
		return core.View{}
	})

	player := modding.NewWorkshop(hub.AsUser(2002), mgr, modding.WorkshopOptions{
		AppID:    480,
		AutoLoad: true,
	})
	for _, id := range []modding.PublishedFileID{addonPub.ID, basePub.ID, futurePub.ID} {
		if err := player.Subscribe(ctx, id); err != nil {
			t.Fatalf("subscribe %s: %v", id, err)
		}
	}

	var syncRes modding.SyncResult
	app.Post(func() {
		syncRes, err = player.SyncSubscribed(ctx)
	})
	app.Settle()

	// Incompatible future-mod should be reported as ErrVersionMismatch while valid mods install & load!
	if !errors.Is(err, modding.ErrVersionMismatch) {
		t.Fatalf("SyncSubscribed err = %v, want ErrVersionMismatch for future-mod", err)
	}
	if len(syncRes.Installed) != 2 {
		t.Fatalf("Installed = %+v, want 2 valid mods", syncRes.Installed)
	}
	rawOrder, ok := mgr.GetState("order")
	if !ok {
		t.Fatal("missing load order state")
	}
	seq, ok := rawOrder.([]any)
	if !ok || len(seq) != 2 || seq[0] != "level-core" || seq[1] != "arena-level" {
		t.Fatalf("load order = %#v, want [level-core arena-level]", rawOrder)
	}
}

func TestWorkshopInstallSubscribedAndPathConfinement(t *testing.T) {
	ctx := context.Background()
	wsRoot := t.TempDir()
	hub := modding.NewMemoryWorkshop(wsRoot)
	author := modding.NewWorkshop(hub.AsUser(1001), nil, modding.WorkshopOptions{AppID: 480})

	pub, err := author.Publish(ctx, copySampleBossRushMod(t), modding.PublishOptions{})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	var mgr *modding.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{GameVersion: "0.2.0"})
		return core.View{}
	})

	// 1. Disallowed AllowedRoots refuses installation with ErrWorkshopUnsafePath.
	otherRoot := t.TempDir()
	confinedWS := modding.NewWorkshop(hub.AsUser(2002), mgr, modding.WorkshopOptions{
		AppID:        480,
		AllowedRoots: []string{otherRoot},
	})
	if err := confinedWS.Subscribe(ctx, pub.ID); err != nil {
		t.Fatal(err)
	}
	app.Post(func() {
		_, err = confinedWS.InstallSubscribed(ctx, pub.ID)
	})
	app.Settle()
	if !errors.Is(err, modding.ErrWorkshopUnsafePath) {
		t.Fatalf("InstallSubscribed outside AllowedRoots err = %v, want ErrWorkshopUnsafePath", err)
	}

	// 2. Allowed root permits InstallSubscribed.
	okWS := modding.NewWorkshop(hub.AsUser(2002), mgr, modding.WorkshopOptions{
		AppID:        480,
		AllowedRoots: []string{wsRoot},
		AutoLoad:     true,
	})
	var installed modding.InstalledWorkshopItem
	app.Post(func() {
		installed, err = okWS.InstallSubscribed(ctx, pub.ID)
	})
	app.Settle()
	if err != nil {
		t.Fatalf("InstallSubscribed within AllowedRoots failed: %v", err)
	}
	if installed.ModID != "com.example.boss-rush" {
		t.Fatalf("installed = %+v", installed)
	}

	// 3. Symlink escaping mod directory is refused before loading.
	outsideFile := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkPath := filepath.Join(installed.Folder, "escape.txt")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Skipf("symlink not supported on this platform: %v", err)
	}
	app.Post(func() {
		_, err = okWS.InstallSubscribed(ctx, pub.ID)
	})
	app.Settle()
	if !errors.Is(err, modding.ErrWorkshopUnsafePath) {
		t.Fatalf("InstallSubscribed with escaping symlink err = %v, want ErrWorkshopUnsafePath", err)
	}
}

func TestWorkshopMetadataYAMLStrictAndDeterministic(t *testing.T) {
	in := modding.WorkshopMetadata{
		AppID:           480,
		PublishedFileID: 123456789,
		Title:           "Crystal Caverns",
		Description:     "Custom dungeon level pack.",
		Tags:            []string{"level", "dungeon"},
		Visibility:      modding.VisibilityUnlisted,
		Preview:         "assets/preview.png",
	}
	encoded, err := modding.EncodeWorkshopMetadata(in)
	if err != nil {
		t.Fatalf("EncodeWorkshopMetadata: %v", err)
	}
	want := `version: 1
appId: 480
publishedFileId: 123456789
title: Crystal Caverns
description: Custom dungeon level pack.
tags: [dungeon, level]
visibility: unlisted
preview: assets/preview.png
`
	if string(encoded) != want {
		t.Fatalf("encoded workshop.yaml:\n%s\nwant:\n%s", encoded, want)
	}

	parsed, err := modding.ParseWorkshopMetadata(encoded)
	if err != nil {
		t.Fatalf("ParseWorkshopMetadata: %v", err)
	}
	again, err := modding.EncodeWorkshopMetadata(parsed)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if !bytes.Equal(encoded, again) {
		t.Fatalf("round-trip mismatch:\n%s\nvs\n%s", encoded, again)
	}

	invalidDocs := map[string]string{
		"empty":             "",
		"missing version":   "appId: 480\n",
		"future version":    "version: 2\nappId: 480\n",
		"float version":     "version: 1.0\nappId: 480\n",
		"hex version":       "version: 0x1\nappId: 480\n",
		"unknown field":     "version: 1\nappId: 480\nunknownKey: yes\n",
		"duplicate key":     "version: 1\nappId: 480\nappId: 481\n",
		"trailing document": "version: 1\nappId: 480\n---\nversion: 1\nappId: 480\n",
		"zero appId":        "version: 1\nappId: 0\n",
		"bad visibility":    "version: 1\nappId: 480\nvisibility: hidden\n",
		"escaping preview":  "version: 1\nappId: 480\npreview: ../evil.png\n",
		"absolute preview":  "version: 1\nappId: 480\npreview: /etc/passwd\n",
		"backslash preview": "version: 1\nappId: 480\npreview: img\\p.png\n",
		"empty tag":         "version: 1\nappId: 480\ntags: [level, '']\n",
		"duplicate tag":     "version: 1\nappId: 480\ntags: [level, level]\n",
		"padded tag":        "version: 1\nappId: 480\ntags: [' level ']\n",
	}
	for name, doc := range invalidDocs {
		t.Run(name, func(t *testing.T) {
			if _, err := modding.ParseWorkshopMetadata([]byte(doc)); !errors.Is(err, modding.ErrInvalidWorkshopMetadata) {
				t.Fatalf("ParseWorkshopMetadata(%s) err = %v, want ErrInvalidWorkshopMetadata", name, err)
			}
		})
	}
}

func TestSteamWorkshopUnavailable(t *testing.T) {
	_, err := modding.NewSteamWorkshop(modding.SteamWorkshopOptions{
		LibraryPath: filepath.Join(t.TempDir(), "missing_libsteam_api"),
	})
	if !errors.Is(err, modding.ErrWorkshopUnavailable) {
		t.Fatalf("NewSteamWorkshop err = %v, want ErrWorkshopUnavailable", err)
	}

	ctx := context.Background()
	ws := modding.NewWorkshop(nil, nil, modding.WorkshopOptions{AppID: 480})
	if _, err := ws.Publish(ctx, t.TempDir(), modding.PublishOptions{}); !errors.Is(err, modding.ErrWorkshopUnavailable) {
		t.Fatalf("Publish(nil backend) err = %v, want ErrWorkshopUnavailable", err)
	}
	if err := ws.Subscribe(ctx, 1); !errors.Is(err, modding.ErrWorkshopUnavailable) {
		t.Fatalf("Subscribe(nil backend) err = %v, want ErrWorkshopUnavailable", err)
	}
	if err := ws.Unsubscribe(ctx, 1); !errors.Is(err, modding.ErrWorkshopUnavailable) {
		t.Fatalf("Unsubscribe(nil backend) err = %v, want ErrWorkshopUnavailable", err)
	}
	if _, err := ws.QueryItems(ctx, modding.WorkshopQuery{}); !errors.Is(err, modding.ErrWorkshopUnavailable) {
		t.Fatalf("QueryItems(nil backend) err = %v, want ErrWorkshopUnavailable", err)
	}
	if _, err := ws.SyncSubscribed(ctx); !errors.Is(err, modding.ErrWorkshopUnavailable) {
		t.Fatalf("SyncSubscribed(nil backend) err = %v, want ErrWorkshopUnavailable", err)
	}
	if _, err := ws.InstallSubscribed(ctx, 1); !errors.Is(err, modding.ErrWorkshopUnavailable) {
		t.Fatalf("InstallSubscribed(nil backend) err = %v, want ErrWorkshopUnavailable", err)
	}
}

func TestManagerInstallAndUninstall(t *testing.T) {
	dir := copySampleBossRushMod(t)
	var mgr *modding.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{GameVersion: "0.2.0"})
		return core.View{}
	})

	var (
		manifest modding.ModManifest
		err      error
	)
	app.Post(func() {
		manifest, err = mgr.Install(dir)
		if err == nil {
			err = mgr.Load(manifest.ID)
		}
	})
	app.Settle()
	if err != nil {
		t.Fatalf("Install+Load failed: %v", err)
	}
	if info, ok := mgr.Info("com.example.boss-rush"); !ok || info.State != modding.StateLoaded {
		t.Fatalf("unexpected info after Install+Load: %+v, %v", info, ok)
	}

	app.Post(func() {
		err = mgr.Uninstall("com.example.boss-rush")
	})
	app.Settle()
	if err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}
	if _, ok := mgr.Info("com.example.boss-rush"); ok {
		t.Fatal("mod still present after Uninstall")
	}
	if err := mgr.Uninstall("com.example.boss-rush"); !errors.Is(err, modding.ErrNotFound) {
		t.Fatalf("second Uninstall err = %v, want ErrNotFound", err)
	}
}

func mustReadString(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}
