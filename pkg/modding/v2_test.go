package modding_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/modding"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const fullV2Manifest = `manifestVersion: 2
id: com.example.boss-rush
name: Boss Rush
version: 1.3.0
author: Example Studio
description: Boss rush expansion with optional music and assets.
entry: mod.tsx
gameVersion: '>=0.2.0'
capabilities: [readState, spawnEntity]
dependencies:
  com.example.core: ^1.0.0
optionalDependencies:
  com.example.music: '>=0.2.0'
conflicts:
  com.example.old-boss: '*'
platforms: [linux, macos, windows]
contributes:
  assets: assets
  scenes: scenes
  l10n: locales
  datatables: tables
`

func TestManifestV2ParseEncodeRoundTrip(t *testing.T) {
	got, err := modding.ParseManifest([]byte(fullV2Manifest))
	if err != nil {
		t.Fatalf("ParseManifest(v2): %v", err)
	}
	want := modding.ModManifest{
		ManifestVersion:      modding.ManifestVersion,
		ID:                   "com.example.boss-rush",
		Name:                 "Boss Rush",
		Version:              "1.3.0",
		Author:               "Example Studio",
		Description:          "Boss rush expansion with optional music and assets.",
		Entry:                "mod.tsx",
		GameVersion:          ">=0.2.0",
		Capabilities:         modding.CapReadState | modding.CapSpawnEntity,
		Dependencies:         map[string]string{"com.example.core": "^1.0.0"},
		OptionalDependencies: map[string]string{"com.example.music": ">=0.2.0"},
		Conflicts:            map[string]string{"com.example.old-boss": "*"},
		Platforms:            []string{"linux", "macos", "windows"},
		Contributes: modding.ModContributes{
			Assets:     "assets",
			Scenes:     "scenes",
			L10n:       "locales",
			DataTables: "tables",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseManifest = %+v, want %+v", got, want)
	}

	out, err := modding.EncodeManifest(got)
	if err != nil {
		t.Fatalf("EncodeManifest(v2): %v", err)
	}
	if string(out) != fullV2Manifest {
		t.Fatalf("EncodeManifest(v2) differs:\n%s\nwant:\n%s", out, fullV2Manifest)
	}

	// Verify deterministic sorting of optionalDependencies, conflicts, and platforms regardless of input order.
	unsorted := modding.ModManifest{
		ManifestVersion:      2,
		ID:                   "com.example.sort",
		Name:                 "Sort",
		Version:              "1.0.0",
		Entry:                "mod.js",
		OptionalDependencies: map[string]string{"mod.v9": "*", "mod.v10": "*", "Mod.A": "^1.0.0"},
		Conflicts:            map[string]string{"z.conflict": "*", "a.conflict": "<2.0.0"},
		Platforms:            []string{"windows", "macos", "linux"},
	}
	wantSorted := `manifestVersion: 2
id: com.example.sort
name: Sort
version: 1.0.0
entry: mod.js
optionalDependencies:
  Mod.A: ^1.0.0
  mod.v10: '*'
  mod.v9: '*'
conflicts:
  a.conflict: <2.0.0
  z.conflict: '*'
platforms: [linux, macos, windows]
`
	for i := range 5 {
		enc, err := modding.EncodeManifest(unsorted)
		if err != nil {
			t.Fatalf("EncodeManifest(unsorted): %v", err)
		}
		if string(enc) != wantSorted {
			t.Fatalf("encode %d:\n%s\nwant:\n%s", i, enc, wantSorted)
		}
	}
}

func TestManifestV2ContentOnlyRoundTrip(t *testing.T) {
	raw := `manifestVersion: 2
id: com.example.textures
name: HD Textures
version: 1.0.0
contributes:
  assets: art/hd
  l10n: locales
`
	m, err := modding.ParseManifest([]byte(raw))
	if err != nil {
		t.Fatalf("ParseManifest(content-only): %v", err)
	}
	if m.Entry != "" {
		t.Fatalf("Entry = %q, want empty", m.Entry)
	}
	out, err := modding.EncodeManifest(m)
	if err != nil {
		t.Fatalf("EncodeManifest(content-only): %v", err)
	}
	if string(out) != raw {
		t.Fatalf("round-trip mismatch:\n%s\nwant:\n%s", out, raw)
	}
}

func TestManifestV1RefusesV2Fields(t *testing.T) {
	const base = "manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\n"
	cases := map[string]struct {
		doc   string
		field string
	}{
		"optionalDependencies":       {base + "optionalDependencies:\n  b: ^1.0.0\n", "optionalDependencies"},
		"empty optionalDependencies": {base + "optionalDependencies: {}\n", "optionalDependencies"},
		"conflicts":                  {base + "conflicts:\n  b: '*'\n", "conflicts"},
		"empty conflicts":            {base + "conflicts: {}\n", "conflicts"},
		"platforms":                  {base + "platforms: [macos]\n", "platforms"},
		"empty platforms":            {base + "platforms: []\n", "platforms"},
		"contributes":                {base + "contributes:\n  assets: assets\n", "contributes"},
		"empty contributes":          {base + "contributes: {}\n", "contributes"},
		"missing entry in v1":        {"manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\n", "entry"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			data := []byte(tc.doc)
			_, err := modding.ParseManifest(data)
			if !errors.Is(err, modding.ErrInvalidManifest) {
				t.Fatalf("ParseManifest error = %v, want ErrInvalidManifest", err)
			}
			e, ok := loaderr.As(err)
			if !ok {
				t.Fatalf("error is not *loaderr.Error: %v", err)
			}
			if e.Field != tc.field {
				t.Errorf("Field = %q, want %q (err: %v)", e.Field, tc.field, err)
			}
		})
	}
}

func TestManifestV2ValidationErrors(t *testing.T) {
	const base = "manifestVersion: 2\nid: com.example.a\nname: A\nversion: 1.0.0\nentry: mod.js\n"
	cases := []struct {
		name     string
		doc      string
		field    string
		sentinel error
	}{
		{"float v2 literal", strings.Replace(base, "manifestVersion: 2", "manifestVersion: 2.0", 1), "manifestVersion", modding.ErrInvalidManifest},
		{"hex v2 literal", strings.Replace(base, "manifestVersion: 2", "manifestVersion: 0x2", 1), "manifestVersion", modding.ErrInvalidManifest},
		{"no entry and no contributes", "manifestVersion: 2\nid: com.example.a\nname: A\nversion: 1.0.0\n", "entry", modding.ErrInvalidManifest},
		{"self optional dependency", base + "optionalDependencies:\n  com.example.a: ^1.0.0\n", loaderr.Path("optionalDependencies", "com.example.a"), modding.ErrCyclicDependency},
		{"dep in both required and optional", base + "dependencies:\n  com.example.b: ^1.0.0\noptionalDependencies:\n  com.example.b: ^1.0.0\n", loaderr.Path("optionalDependencies", "com.example.b"), modding.ErrInvalidManifest},
		{"bad optional constraint", base + "optionalDependencies:\n  com.example.b: nope\n", loaderr.Path("optionalDependencies", "com.example.b"), modding.ErrInvalidManifest},
		{"self conflict", base + "conflicts:\n  com.example.a: '*'\n", loaderr.Path("conflicts", "com.example.a"), modding.ErrInvalidManifest},
		{"bad conflict constraint", base + "conflicts:\n  com.example.b: '>>1'\n", loaderr.Path("conflicts", "com.example.b"), modding.ErrInvalidManifest},
		{"unknown platform", base + "platforms: [freebsd]\n", "platforms", modding.ErrInvalidManifest},
		{"duplicate platform", base + "platforms: [macos, macos]\n", "platforms", modding.ErrInvalidManifest},
		{"absolute contributes", base + "contributes:\n  assets: /assets\n", "contributes.assets", modding.ErrInvalidManifest},
		{"escaping contributes", base + "contributes:\n  scenes: ../scenes\n", "contributes.scenes", modding.ErrInvalidManifest},
		{"nested dotdot contributes", base + "contributes:\n  l10n: a/../b\n", "contributes.l10n", modding.ErrInvalidManifest},
		{"backslash contributes", base + "contributes:\n  datatables: sub\\tables\n", "contributes.datatables", modding.ErrInvalidManifest},
		{"dot contributes", base + "contributes:\n  assets: .\n", "contributes.assets", modding.ErrInvalidManifest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.doc)
			_, err := modding.ParseManifest(data)
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("ParseManifest error = %v, want %v", err, tc.sentinel)
			}
			loaderrtest.ExpectField(t, loaderr.WithFile(err, "mod/mod.yaml"), "mod/mod.yaml", data, tc.field)
		})
	}
}

func TestResolveOrderOptionalDependenciesAndConflicts(t *testing.T) {
	t.Run("optional dependency present loads first", func(t *testing.T) {
		mods := []modding.ModManifest{
			{
				ManifestVersion:      2,
				ID:                   "alpha",
				Name:                 "Alpha",
				Version:              "1.0.0",
				Entry:                "mod.js",
				OptionalDependencies: map[string]string{"zeta": ">=0.2.0"},
			},
			{ManifestVersion: 2, ID: "zeta", Name: "Zeta", Version: "0.3.0", Entry: "mod.js"},
		}
		ordered, err := modding.ResolveOrder(mods)
		if err != nil {
			t.Fatalf("ResolveOrder: %v", err)
		}
		if len(ordered) != 2 || ordered[0].ID != "zeta" || ordered[1].ID != "alpha" {
			t.Fatalf("order = %v, want [zeta alpha]", []string{ordered[0].ID, ordered[1].ID})
		}
	})

	t.Run("optional dependency absent is ignored", func(t *testing.T) {
		mods := []modding.ModManifest{
			{
				ManifestVersion:      2,
				ID:                   "alpha",
				Name:                 "Alpha",
				Version:              "1.0.0",
				Entry:                "mod.js",
				OptionalDependencies: map[string]string{"missing": ">=0.2.0"},
			},
		}
		ordered, err := modding.ResolveOrder(mods)
		if err != nil {
			t.Fatalf("ResolveOrder: %v", err)
		}
		if len(ordered) != 1 || ordered[0].ID != "alpha" {
			t.Fatalf("order = %+v, want [alpha]", ordered)
		}
	})

	t.Run("optional dependency version mismatch fails", func(t *testing.T) {
		mods := []modding.ModManifest{
			{
				ManifestVersion:      2,
				ID:                   "alpha",
				Name:                 "Alpha",
				Version:              "1.0.0",
				Entry:                "mod.js",
				OptionalDependencies: map[string]string{"zeta": ">=2.0.0"},
			},
			{ManifestVersion: 2, ID: "zeta", Name: "Zeta", Version: "1.0.0", Entry: "mod.js"},
		}
		if _, err := modding.ResolveOrder(mods); !errors.Is(err, modding.ErrVersionMismatch) {
			t.Fatalf("ResolveOrder error = %v, want ErrVersionMismatch", err)
		}
	})

	t.Run("optional dependency cycle fails", func(t *testing.T) {
		mods := []modding.ModManifest{
			{
				ManifestVersion: 2,
				ID:              "a",
				Name:            "A",
				Version:         "1.0.0",
				Entry:           "mod.js",
				Dependencies:    map[string]string{"b": "*"},
			},
			{
				ManifestVersion:      2,
				ID:                   "b",
				Name:                 "B",
				Version:              "1.0.0",
				Entry:                "mod.js",
				OptionalDependencies: map[string]string{"a": "*"},
			},
		}
		if _, err := modding.ResolveOrder(mods); !errors.Is(err, modding.ErrCyclicDependency) {
			t.Fatalf("ResolveOrder error = %v, want ErrCyclicDependency", err)
		}
	})

	t.Run("conflict refused in either direction", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			mods []modding.ModManifest
		}{
			{
				name: "first declares conflict with second",
				mods: []modding.ModManifest{
					{ManifestVersion: 2, ID: "a", Name: "A", Version: "1.0.0", Entry: "mod.js", Conflicts: map[string]string{"b": "*"}},
					{ManifestVersion: 2, ID: "b", Name: "B", Version: "1.0.0", Entry: "mod.js"},
				},
			},
			{
				name: "second declares conflict with first",
				mods: []modding.ModManifest{
					{ManifestVersion: 2, ID: "a", Name: "A", Version: "1.2.0", Entry: "mod.js"},
					{ManifestVersion: 2, ID: "b", Name: "B", Version: "1.0.0", Entry: "mod.js", Conflicts: map[string]string{"a": "^1.0.0"}},
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := modding.ResolveOrder(tc.mods); !errors.Is(err, modding.ErrConflictingMods) {
					t.Fatalf("ResolveOrder error = %v, want ErrConflictingMods", err)
				}
			})
		}
	})

	t.Run("conflict outside constraint or absent is allowed", func(t *testing.T) {
		mods := []modding.ModManifest{
			{
				ManifestVersion: 2,
				ID:              "a",
				Name:            "A",
				Version:         "1.0.0",
				Entry:           "mod.js",
				Conflicts:       map[string]string{"b": "<2.0.0", "absent": "*"},
			},
			{ManifestVersion: 2, ID: "b", Name: "B", Version: "2.0.0", Entry: "mod.js"},
		}
		ordered, err := modding.ResolveOrder(mods)
		if err != nil {
			t.Fatalf("ResolveOrder: %v", err)
		}
		if len(ordered) != 2 || ordered[0].ID != "a" || ordered[1].ID != "b" {
			t.Fatalf("order = %+v, want [a b]", ordered)
		}
	})
}

func TestDiscoverFSIsolatesPerModFailures(t *testing.T) {
	fsys := modFS(map[string]string{
		// Healthy script mod
		"good/mod.yaml": `manifestVersion: 2
id: good-mod
name: Good Mod
version: 1.0.0
entry: mod.js
capabilities: [writeState]
`,
		"good/mod.js": `import { setState } from "game"; setState("goodLoaded", true);`,

		// Healthy mod with optional dep on a failed (unsupported-platform) mod -> MUST still load!
		"opt-user/mod.yaml": `manifestVersion: 2
id: opt-user
name: Optional User
version: 1.0.0
entry: mod.js
capabilities: [writeState]
optionalDependencies:
  win-only: '*'
`,
		"opt-user/mod.js": `import { setState } from "game"; setState("optUserLoaded", true);`,

		// Bad YAML syntax
		"broken-yaml/mod.yaml": "manifestVersion: 2\nid: [broken\n",

		// Missing entry file
		"missing-entry/mod.yaml": "manifestVersion: 2\nid: missing-entry\nname: Missing Entry\nversion: 1.0.0\nentry: nope.js\n",

		// Unsupported platform (host is macos)
		"win-only/mod.yaml": "manifestVersion: 2\nid: win-only\nname: Win Only\nversion: 1.0.0\nentry: mod.js\nplatforms: [windows, linux]\n",
		"win-only/mod.js":   "export {};",

		// Dependent of a failed mod -> must fail with ErrModFailed
		"needs-win/mod.yaml": "manifestVersion: 2\nid: needs-win\nname: Needs Win\nversion: 1.0.0\nentry: mod.js\ndependencies:\n  win-only: '*'\n",
		"needs-win/mod.js":   "export {};",

		// Duplicate IDs -> both fail with ErrDuplicateMod
		"dup1/mod.yaml": "manifestVersion: 2\nid: dup-mod\nname: Dup 1\nversion: 1.0.0\nentry: mod.js\n",
		"dup1/mod.js":   "export {};",
		"dup2/mod.yaml": "manifestVersion: 2\nid: dup-mod\nname: Dup 2\nversion: 1.0.0\nentry: mod.js\n",
		"dup2/mod.js":   "export {};",

		// Conflicting pair -> both fail with ErrConflictingMods
		"conf-a/mod.yaml": "manifestVersion: 2\nid: conf-a\nname: Conf A\nversion: 1.0.0\nentry: mod.js\nconflicts:\n  conf-b: '*'\n",
		"conf-a/mod.js":   "export {};",
		"conf-b/mod.yaml": "manifestVersion: 2\nid: conf-b\nname: Conf B\nversion: 1.0.0\nentry: mod.js\n",
		"conf-b/mod.js":   "export {};",
	})

	var mgr *modding.Manager
	var discovered []modding.ModManifest
	var discoverErr error
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{Platform: "macos"})
		discovered, discoverErr = mgr.DiscoverFS(fsys)
		if err := mgr.LoadAll(); err != nil {
			t.Errorf("LoadAll failed on healthy mods: %v", err)
		}
		return core.View{}
	})

	if discoverErr == nil {
		t.Fatal("DiscoverFS returned nil error despite failing mods")
	}
	// Only the two healthy mods should be discovered and loaded.
	gotIDs := make([]string, len(discovered))
	for i, m := range discovered {
		gotIDs[i] = m.ID
	}
	if !slices.Equal(gotIDs, []string{"good-mod", "opt-user"}) {
		t.Fatalf("discovered = %v, want [good-mod opt-user]", gotIDs)
	}
	if v, _ := mgr.GetState("goodLoaded"); v != true {
		t.Fatalf("goodLoaded = %v, want true", v)
	}
	if v, _ := mgr.GetState("optUserLoaded"); v != true {
		t.Fatalf("optUserLoaded = %v, want true", v)
	}

	// Check each failed mod's state and sentinel error via Info and List.
	wantFailed := map[string]error{
		"missing-entry": modding.ErrInvalidManifest,
		"win-only":      modding.ErrUnsupportedPlatform,
		"needs-win":     modding.ErrModFailed,
		"dup-mod":       modding.ErrDuplicateMod,
		"conf-a":        modding.ErrConflictingMods,
		"conf-b":        modding.ErrConflictingMods,
	}
	for id, wantSentinel := range wantFailed {
		info, ok := mgr.Info(id)
		if !ok {
			t.Fatalf("Info(%q) missing", id)
		}
		if info.State != modding.StateFailed || !errors.Is(info.Error, wantSentinel) {
			t.Errorf("Info(%q) = (state=%s, err=%v), want StateFailed with %v", id, info.State, info.Error, wantSentinel)
		}
		// Calling Load directly on a mod that failed during discovery must keep it failed.
		if err := mgr.Load(id); !errors.Is(err, wantSentinel) {
			t.Errorf("Load(%q) = %v, want %v", id, err, wantSentinel)
		}
	}

	// List must include both duplicate dirs (dup1, dup2) and the unparseable broken-yaml dir.
	list := mgr.List()
	failedByDir := map[string]modding.ModInfo{}
	for _, info := range list {
		if info.State == modding.StateFailed {
			failedByDir[info.Dir] = info
		}
	}
	for _, dir := range []string{"broken-yaml", "missing-entry", "win-only", "needs-win", "dup1", "dup2", "conf-a", "conf-b"} {
		info, ok := failedByDir[dir]
		if !ok || info.Error == nil {
			t.Errorf("List missing StateFailed entry for dir %q (got %+v)", dir, info)
		}
	}
}

func TestManagerContentFSOverlayAndOverrideOrder(t *testing.T) {
	fsys := modFS(map[string]string{
		"base/mod.yaml": `manifestVersion: 2
id: base-pack
name: Base Pack
version: 1.0.0
contributes:
  assets: assets
  scenes: scenes
  l10n: locales
  datatables: tables
`,
		"base/assets/sprites/hero.txt": "base-hero",
		"base/assets/sprites/tree.txt": "base-tree",
		"base/scenes/intro.scene.yaml": "scene-base",
		"base/locales/en.yaml":         "version: 1\nlocale: en\nstrings:\n  title: Base Game\n",
		"base/tables/items.yaml":       "version: 1\nrows: []\n",

		// patch depends on base-pack -> loads AFTER base-pack, so patch wins on shared paths!
		"patch/mod.yaml": `manifestVersion: 2
id: a-patch-pack
name: Patch Pack
version: 1.0.0
dependencies:
  base-pack: ^1.0.0
contributes:
  assets: art
  l10n: i18n
`,
		"patch/art/sprites/hero.txt": "patched-hero",
		"patch/art/sprites/hat.txt":  "patched-hat",
		"patch/i18n/en.yaml":         "version: 1\nlocale: en\nstrings:\n  title: Patched Game\n",

		// opt-addon optionally depends on a-patch-pack -> loads LAST and wins over patch!
		"opt/mod.yaml": `manifestVersion: 2
id: a-final-pack
name: Final Pack
version: 1.0.0
optionalDependencies:
  a-patch-pack: '*'
contributes:
  assets: custom_assets
`,
		"opt/custom_assets/sprites/hero.txt": "final-hero",
	})

	var mgr *modding.Manager
	var assetMgr *assets.Manager
	var loadedAsset []byte
	loadDone := make(chan error, 1)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{})
		got, err := mgr.DiscoverFS(fsys)
		if err != nil {
			t.Fatalf("DiscoverFS: %v", err)
		}
		if len(got) != 3 || got[0].ID != "base-pack" || got[1].ID != "a-patch-pack" || got[2].ID != "a-final-pack" {
			t.Fatalf("load order = %+v, want [base-pack a-patch-pack a-final-pack]", got)
		}
		if err := mgr.LoadAll(); err != nil {
			t.Fatalf("LoadAll: %v", err)
		}

		// Verify integration with assets.WithFS using fs.Sub(mgr.ContentFS(), "assets").
		assetsSub, err := fs.Sub(mgr.ContentFS(), "assets")
		if err != nil {
			t.Fatalf("fs.Sub(assets): %v", err)
		}
		assetMgr = assets.NewManager(cx, assets.WithFS(assetsSub))
		g := assetMgr.Group("modded")
		g.AddBytes("hero", "asset:sprites/hero.txt")
		assetMgr.LoadGroup("modded", func(err error) {
			loadedAsset, _ = assetMgr.Bytes("hero")
			loadDone <- err
		})
		return core.View{}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		app.Settle()
		select {
		case err := <-loadDone:
			if err != nil {
				t.Fatalf("assets.LoadGroup: %v", err)
			}
			goto loaded
		default:
			if time.Now().After(deadline) {
				t.Fatal("timed out waiting for assets.LoadGroup")
			}
			runtime.Gosched()
		}
	}
loaded:

	contentFS := mgr.ContentFS()

	// Run Go stdlib fstest.TestFS conformance across the entire overlay!
	if err := fstest.TestFS(contentFS,
		"assets/sprites/hero.txt",
		"assets/sprites/tree.txt",
		"assets/sprites/hat.txt",
		"scenes/intro.scene.yaml",
		"l10n/en.yaml",
		"datatables/items.yaml",
	); err != nil {
		t.Fatalf("fstest.TestFS(ContentFS): %v", err)
	}

	// Verify override order: final-pack > patch-pack > base-pack.
	checkFile := func(p, want string) {
		t.Helper()
		b, err := fs.ReadFile(contentFS, p)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", p, err)
		}
		if string(b) != want {
			t.Errorf("ReadFile(%q) = %q, want %q", p, b, want)
		}
	}
	checkFile("assets/sprites/hero.txt", "final-hero")
	checkFile("assets/sprites/hat.txt", "patched-hat")
	checkFile("assets/sprites/tree.txt", "base-tree")
	checkFile("scenes/intro.scene.yaml", "scene-base")

	// Verify assets.WithFS loaded the winning override.
	if string(loadedAsset) != "final-hero" {
		t.Fatalf("assets.Manager loaded %q, want final-hero", loadedAsset)
	}

	// Verify l10n loader reads the overridden string table directly from ContentFS.
	l10nBytes, err := fs.ReadFile(contentFS, "l10n/en.yaml")
	if err != nil {
		t.Fatalf("ReadFile(l10n/en.yaml): %v", err)
	}
	cat := l10n.NewCatalog("en")
	if err := cat.LoadYAML(l10nBytes); err != nil {
		t.Fatalf("l10n.LoadYAML: %v", err)
	}
	if got := cat.T("en", "title"); got != "Patched Game" {
		t.Fatalf("l10n T(title) = %q, want Patched Game", got)
	}

	// Verify ReadDir merges entries from all loaded mods in sorted order without duplicates.
	entries, err := fs.ReadDir(contentFS, "assets/sprites")
	if err != nil {
		t.Fatalf("ReadDir(assets/sprites): %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"hat.txt", "hero.txt", "tree.txt"}) {
		t.Fatalf("ReadDir(assets/sprites) = %v, want [hat.txt hero.txt tree.txt]", names)
	}

	// Unload the final pack -> hero.txt immediately falls back to patch-pack.
	app.Post(func() {
		if err := mgr.Unload("a-final-pack"); err != nil {
			t.Errorf("Unload(a-final-pack): %v", err)
		}
	})
	app.Settle()
	checkFile("assets/sprites/hero.txt", "patched-hero")

	// Disable patch-pack -> hero.txt falls back to base-pack and hat.txt disappears.
	app.Post(func() {
		if err := mgr.Disable("a-patch-pack"); err != nil {
			t.Errorf("Disable(a-patch-pack): %v", err)
		}
	})
	app.Settle()
	checkFile("assets/sprites/hero.txt", "base-hero")
	if _, err := fs.ReadFile(contentFS, "assets/sprites/hat.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadFile(hat.txt) after disabling patch = %v, want fs.ErrNotExist", err)
	}

	// Close clears ContentFS completely.
	app.Post(func() { mgr.Close() })
	app.Settle()
	if rootEntries, err := fs.ReadDir(contentFS, "."); err != nil || len(rootEntries) != 0 {
		t.Fatalf("ReadDir(.) after Close = (%v, %v), want empty", rootEntries, err)
	}
}

func TestManagerContentFSPathConfinementAndSymlinks(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("symlinks require OS filesystem")
	}
	root := t.TempDir()
	modsDir := filepath.Join(root, "mods")
	secretFile := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("top-secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Healthy mod with a safe internal symlink inside assets/.
	goodDir := filepath.Join(modsDir, "good")
	if err := os.MkdirAll(filepath.Join(goodDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(goodDir, "mod.yaml"), "manifestVersion: 2\nid: good-pack\nname: Good Pack\nversion: 1.0.0\ncontributes:\n  assets: assets\n")
	writeFile(t, filepath.Join(goodDir, "assets", "real.txt"), "safe-data")
	if err := os.Symlink("real.txt", filepath.Join(goodDir, "assets", "alias.txt")); err != nil {
		t.Skipf("symlink creation unsupported: %v", err)
	}

	// 2. Mod with relative escaping symlink discovered at scan time.
	relEscapeDir := filepath.Join(modsDir, "rel-escape")
	if err := os.MkdirAll(filepath.Join(relEscapeDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(relEscapeDir, "mod.yaml"), "manifestVersion: 2\nid: rel-escape\nname: Rel Escape\nversion: 1.0.0\ncontributes:\n  assets: assets\n")
	if err := os.Symlink(filepath.Join("..", "..", "..", "secret.txt"), filepath.Join(relEscapeDir, "assets", "leak.txt")); err != nil {
		t.Fatal(err)
	}

	// 3. Mod with absolute escaping symlink discovered at scan time.
	absEscapeDir := filepath.Join(modsDir, "abs-escape")
	if err := os.MkdirAll(filepath.Join(absEscapeDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(absEscapeDir, "mod.yaml"), "manifestVersion: 2\nid: abs-escape\nname: Abs Escape\nversion: 1.0.0\ncontributes:\n  assets: assets\n")
	if err := os.Symlink(secretFile, filepath.Join(absEscapeDir, "assets", "abs.txt")); err != nil {
		t.Fatal(err)
	}

	// 4. Mod with symlink loop.
	loopDir := filepath.Join(modsDir, "loop")
	if err := os.MkdirAll(filepath.Join(loopDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(loopDir, "mod.yaml"), "manifestVersion: 2\nid: loop-mod\nname: Loop\nversion: 1.0.0\ncontributes:\n  assets: assets\n")
	if err := os.Symlink("b", filepath.Join(loopDir, "assets", "a")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a", filepath.Join(loopDir, "assets", "b")); err != nil {
		t.Fatal(err)
	}

	var mgr *modding.Manager
	var discovered []modding.ModManifest
	var discoverErr error
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{})
		discovered, discoverErr = mgr.Discover(modsDir)
		if err := mgr.LoadAll(); err != nil {
			t.Errorf("LoadAll: %v", err)
		}
		return core.View{}
	})

	if discoverErr == nil {
		t.Fatal("Discover succeeded despite escaping symlinks")
	}
	if len(discovered) != 1 || discovered[0].ID != "good-pack" {
		t.Fatalf("discovered = %+v, want [good-pack]", discovered)
	}
	for _, badID := range []string{"rel-escape", "abs-escape", "loop-mod"} {
		info, ok := mgr.Info(badID)
		if !ok || info.State != modding.StateFailed || !errors.Is(info.Error, modding.ErrUnsafePath) {
			t.Errorf("Info(%q) = (state=%s, err=%v), want StateFailed with ErrUnsafePath", badID, info.State, info.Error)
		}
	}

	contentFS := mgr.ContentFS()

	// Safe internal symlink works.
	if data, err := fs.ReadFile(contentFS, "assets/alias.txt"); err != nil || string(data) != "safe-data" {
		t.Fatalf("ReadFile(assets/alias.txt) = (%q, %v), want safe-data", data, err)
	}

	// Path traversal strings are refused with fs.ErrInvalid.
	for _, badPath := range []string{"../secret.txt", "/etc/passwd", "assets/../../secret.txt", "assets/../mod.yaml", "assets\\..\\secret.txt", ""} {
		if _, err := contentFS.Open(badPath); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Open(%q) = %v, want fs.ErrInvalid", badPath, err)
		}
	}

	// Even if an escaping symlink is dropped into a loaded mod AFTER Discover+LoadAll, ContentFS still refuses it.
	if err := os.Symlink(filepath.Join("..", "..", "..", "secret.txt"), filepath.Join(goodDir, "assets", "late-escape.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.ReadFile(contentFS, "assets/late-escape.txt"); !errors.Is(err, modding.ErrUnsafePath) {
		t.Fatalf("ReadFile(late-escape.txt) = %v, want ErrUnsafePath", err)
	}
	if _, err := fs.ReadDir(contentFS, "assets"); !errors.Is(err, modding.ErrUnsafePath) {
		t.Fatalf("ReadDir(assets) with late escaping symlink = %v, want ErrUnsafePath", err)
	}
}

func TestContentOnlyModReloadUpdatesContributes(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "pack")
	if err := os.MkdirAll(filepath.Join(modDir, "assets_v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(modDir, "assets_v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(modDir, "mod.yaml"), "manifestVersion: 2\nid: pack\nname: Pack\nversion: 1.0.0\ncontributes:\n  assets: assets_v1\n")
	writeFile(t, filepath.Join(modDir, "assets_v1", "banner.txt"), "v1")
	writeFile(t, filepath.Join(modDir, "assets_v2", "banner.txt"), "v2")

	var mgr *modding.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{})
		if _, err := mgr.Discover(dir); err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if err := mgr.LoadAll(); err != nil {
			t.Fatalf("LoadAll: %v", err)
		}
		return core.View{}
	})
	if b, err := fs.ReadFile(mgr.ContentFS(), "assets/banner.txt"); err != nil || !bytes.Equal(b, []byte("v1")) {
		t.Fatalf("before reload = (%q, %v), want v1", b, err)
	}

	writeFile(t, filepath.Join(modDir, "mod.yaml"), "manifestVersion: 2\nid: pack\nname: Pack\nversion: 2.0.0\ncontributes:\n  assets: assets_v2\n")
	app.Post(func() {
		if err := mgr.Reload("pack"); err != nil {
			t.Errorf("Reload: %v", err)
		}
	})
	app.Settle()

	if b, err := fs.ReadFile(mgr.ContentFS(), "assets/banner.txt"); err != nil || !bytes.Equal(b, []byte("v2")) {
		t.Fatalf("after reload = (%q, %v), want v2", b, err)
	}
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
