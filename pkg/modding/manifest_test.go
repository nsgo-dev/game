package modding_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/modding"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const fullManifest = `manifestVersion: 1
id: com.example.boss-rush
name: Boss Rush
version: 1.2.0
author: Example Studio
description: Spawns bosses.
entry: scripts/mod.tsx
gameVersion: '>=0.2.0'
capabilities: [readState, spawnEntity, emitEvent, ui]
dependencies:
  com.example.core-lib: ^1.0.0
  com.example.music: '>=2.0.0 <3.0.0'
`

func TestParseManifestReadsYAML(t *testing.T) {
	got, err := modding.ParseManifest([]byte(fullManifest))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	want := modding.ModManifest{
		ManifestVersion: modding.MinManifestVersion,
		ID:              "com.example.boss-rush",
		Name:            "Boss Rush",
		Version:         "1.2.0",
		Author:          "Example Studio",
		Description:     "Spawns bosses.",
		Entry:           "scripts/mod.tsx",
		GameVersion:     ">=0.2.0",
		Capabilities:    modding.CapReadState | modding.CapSpawnEntity | modding.CapEmitEvent | modding.CapUI,
		Dependencies: map[string]string{
			"com.example.core-lib": "^1.0.0",
			"com.example.music":    ">=2.0.0 <3.0.0",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseManifest = %+v, want %+v", got, want)
	}
}

func TestEncodeManifestIsCanonicalAndRoundTrips(t *testing.T) {
	m, err := modding.ParseManifest([]byte(fullManifest))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	out, err := modding.EncodeManifest(m)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if string(out) != fullManifest {
		t.Fatalf("EncodeManifest output differs from the canonical file:\n%s\nwant:\n%s", out, fullManifest)
	}
	back, err := modding.ParseManifest(out)
	if err != nil {
		t.Fatalf("ParseManifest(EncodeManifest): %v", err)
	}
	if !reflect.DeepEqual(back, m) {
		t.Fatalf("round trip = %+v, want %+v", back, m)
	}
}

func TestEncodeManifestOrdersCapabilitiesAndDependencies(t *testing.T) {
	m := modding.ModManifest{
		ManifestVersion: modding.MinManifestVersion,
		ID:              "sorter",
		Name:            "Sorter",
		Version:         "0.1.0",
		Entry:           "mod.js",
		Capabilities:    modding.CapStorage | modding.CapReadState,
		Dependencies:    map[string]string{"zeta": "*", "alpha": "^1.0.0", "mid": "~0.3.0"},
	}
	want := `manifestVersion: 1
id: sorter
name: Sorter
version: 0.1.0
entry: mod.js
capabilities: [readState, storage]
dependencies:
  alpha: ^1.0.0
  mid: ~0.3.0
  zeta: '*'
`
	for i := range 5 {
		out, err := modding.EncodeManifest(m)
		if err != nil {
			t.Fatalf("EncodeManifest: %v", err)
		}
		if string(out) != want {
			t.Fatalf("encode %d:\n%s\nwant:\n%s", i, out, want)
		}
	}
}

func TestEncodeManifestOmitsEmptyOptionalFields(t *testing.T) {
	m := modding.ModManifest{ManifestVersion: modding.MinManifestVersion, ID: "tiny", Name: "Tiny", Version: "1.0.0", Entry: "mod.js"}
	out, err := modding.EncodeManifest(m)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	want := "manifestVersion: 1\nid: tiny\nname: Tiny\nversion: 1.0.0\nentry: mod.js\n"
	if string(out) != want {
		t.Fatalf("EncodeManifest =\n%s\nwant:\n%s", out, want)
	}
}

func TestEncodeManifestRefusesInvalidManifest(t *testing.T) {
	cases := map[string]modding.ModManifest{
		"future manifestVersion": {ManifestVersion: 3, ID: "a", Name: "A", Version: "1.0.0", Entry: "mod.js"},
		"bad id":                 {ManifestVersion: 1, ID: "../a", Name: "A", Version: "1.0.0", Entry: "mod.js"},
		"undefined capability":   {ManifestVersion: 1, ID: "a", Name: "A", Version: "1.0.0", Entry: "mod.js", Capabilities: 1 << 30},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := modding.EncodeManifest(m); !errors.Is(err, modding.ErrInvalidManifest) {
				t.Fatalf("EncodeManifest error = %v, want ErrInvalidManifest", err)
			}
		})
	}
}

func TestManifestBuiltInCodeOmitsManifestVersion(t *testing.T) {
	m := modding.ModManifest{ID: "a", Name: "A", Version: "1.0.0", Entry: "mod.js"}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	order, err := modding.ResolveOrder([]modding.ModManifest{m})
	if err != nil || len(order) != 1 || order[0].ID != "a" {
		t.Fatalf("ResolveOrder = %v, %v; want [a]", order, err)
	}
	out, err := modding.EncodeManifest(m)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	want := "manifestVersion: 2\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\n"
	if string(out) != want {
		t.Fatalf("EncodeManifest =\n%s\nwant:\n%s", out, want)
	}
}

func TestEncodeManifestSortsDependenciesInByteOrder(t *testing.T) {
	m := modding.ModManifest{
		ManifestVersion: modding.MinManifestVersion,
		ID:              "a",
		Name:            "A",
		Version:         "1.0.0",
		Entry:           "mod.js",
		Dependencies:    map[string]string{"mod.v9": "*", "mod.v10": "*", "Mod.z": "*"},
	}
	out, err := modding.EncodeManifest(m)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	want := "manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\ndependencies:\n  Mod.z: '*'\n  mod.v10: '*'\n  mod.v9: '*'\n"
	if string(out) != want {
		t.Fatalf("EncodeManifest =\n%s\nwant:\n%s", out, want)
	}
}

func TestParseManifestRefusesInvalidDocuments(t *testing.T) {
	const base = "manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\n"
	cases := map[string]struct {
		doc  string
		want string
	}{
		"empty":                   {"  \n", "empty manifest"},
		"unknown field":           {base + "homepage: https://example.com\n", `unknown field "homepage"`},
		"misspelled field":        {strings.Replace(base, "entry:", "entrypoint:", 1), "entrypoint"},
		"missing manifestVersion": {strings.Replace(base, "manifestVersion: 1\n", "", 1), "unsupported manifestVersion 0"},
		"future manifestVersion":  {strings.Replace(base, "manifestVersion: 1", "manifestVersion: 3", 1), "unsupported manifestVersion 3"},
		"float manifestVersion":   {strings.Replace(base, "manifestVersion: 1", "manifestVersion: 1.0", 1), "manifestVersion must be the integer 1"},
		"hex manifestVersion":     {strings.Replace(base, "manifestVersion: 1", "manifestVersion: 0x1", 1), "manifestVersion must be the integer 1"},
		"duplicate key":           {base + "name: B\n", "already defined"},
		"trailing document":       {base + "---\nid: b\n", "trailing YAML document"},
		"json manifest":           {`{"id":"a","name":"A","version":"1.0.0","entry":"mod.js"}`, "unsupported manifestVersion 0"},
		"not a mapping":           {"- a\n- b\n", "cannot use"},
		"unknown capability":      {base + "capabilities: [fly]\n", `unknown capability "fly"`},
		"non-canonical name":      {base + "capabilities: [read_state]\n", `unknown capability "read_state"`},
		"repeated capability":     {base + "capabilities: [ui, ui]\n", `capability "ui" listed twice`},
		"capabilities scalar":     {base + "capabilities: ui\n", "capabilities must be a sequence"},
		"capability not a name":   {base + "capabilities: [1]\n", "capability must be a name"},
		"bad dependency":          {base + "dependencies:\n  b: not-a-version\n", "constraint"},
		"bad version":             {strings.Replace(base, "1.0.0", "1.0", 1), "version"},
		"escaping entry":          {strings.Replace(base, "mod.js", "../mod.js", 1), "escapes"},
		"host cap uppercase":      {base + "capabilities: [Inventory.read]\n", `invalid host capability "Inventory.read"`},
		"host cap segment upper":  {base + "capabilities: [inventory.Read]\n", `invalid host capability "inventory.Read"`},
		"host cap snake_case":     {base + "capabilities: [inventory.read_all]\n", `invalid host capability "inventory.read_all"`},
		"host cap kebab-case":     {base + "capabilities: [inventory.read-all]\n", `invalid host capability "inventory.read-all"`},
		"host cap trailing dot":   {base + "capabilities: [inventory.]\n", `invalid host capability "inventory."`},
		"host cap leading dot":    {base + "capabilities: [.read]\n", `invalid host capability ".read"`},
		"host cap double dot":     {base + "capabilities: [inventory..read]\n", `invalid host capability "inventory..read"`},
		"host cap repeated":       {base + "capabilities: [inventory.read, inventory.read]\n", `capability "inventory.read" listed twice`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := modding.ParseManifest([]byte(tc.doc))
			if !errors.Is(err, modding.ErrInvalidManifest) {
				t.Fatalf("ParseManifest error = %v, want ErrInvalidManifest", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseManifest error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestCapabilityYAMLAcceptsEmptyList(t *testing.T) {
	m, err := modding.ParseManifest([]byte("manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\ncapabilities: []\n"))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Capabilities != modding.CapNone || len(m.HostCapabilities) != 0 {
		t.Fatalf("Capabilities = %v / %v, want none", m.Capabilities, m.HostCapabilities)
	}
}

func TestManifestHostCapabilitiesParseEncodeAndRoundTrip(t *testing.T) {
	// Built-in capabilities stay in canonical table order; host capabilities come after, sorted in byte order.
	input := `manifestVersion: 1
id: com.example.inv
name: Inventory Helper
version: 1.0.0
entry: mod.tsx
capabilities: [scene.edit, ui, inventory.read, readState, editor.contextAction]
`
	m, err := modding.ParseManifest([]byte(input))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Capabilities != modding.CapReadState|modding.CapUI {
		t.Fatalf("Capabilities = %v, want readState,ui", m.Capabilities)
	}
	wantHost := []string{"editor.contextAction", "inventory.read", "scene.edit"}
	if !reflect.DeepEqual(m.HostCapabilities, wantHost) {
		t.Fatalf("HostCapabilities = %v, want %v", m.HostCapabilities, wantHost)
	}

	out, err := modding.EncodeManifest(m)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	wantYAML := `manifestVersion: 1
id: com.example.inv
name: Inventory Helper
version: 1.0.0
entry: mod.tsx
capabilities: [readState, ui, editor.contextAction, inventory.read, scene.edit]
`
	if string(out) != wantYAML {
		t.Fatalf("EncodeManifest =\n%s\nwant:\n%s", out, wantYAML)
	}
	back, err := modding.ParseManifest(out)
	if err != nil {
		t.Fatalf("ParseManifest(EncodeManifest): %v", err)
	}
	if !reflect.DeepEqual(back, m) {
		t.Fatalf("round trip = %+v, want %+v", back, m)
	}

	// Host-only capabilities (no built-in bits) must not be omitted.
	hostOnly := modding.ModManifest{
		ID:               "host.only",
		Name:             "Host Only",
		Version:          "1.0.0",
		Entry:            "mod.ts",
		HostCapabilities: []string{"z.beta", "a.alpha"},
	}
	encoded, err := modding.EncodeManifest(hostOnly)
	if err != nil {
		t.Fatalf("EncodeManifest(hostOnly): %v", err)
	}
	wantHostOnly := "manifestVersion: 2\nid: host.only\nname: Host Only\nversion: 1.0.0\nentry: mod.ts\ncapabilities: [a.alpha, z.beta]\n"
	if string(encoded) != wantHostOnly {
		t.Fatalf("EncodeManifest(hostOnly) =\n%s\nwant:\n%s", encoded, wantHostOnly)
	}

	// Validate refuses invalid or duplicate HostCapabilities built in code.
	for name, bad := range map[string][]string{
		"no dot":    {"readState"},
		"uppercase": {"Inv.read"},
		"duplicate": {"inventory.read", "inventory.read"},
	} {
		t.Run("validate/"+name, func(t *testing.T) {
			bm := modding.ModManifest{ID: "bad", Name: "Bad", Version: "1.0.0", Entry: "mod.ts", HostCapabilities: bad}
			if err := bm.Validate(); !errors.Is(err, modding.ErrInvalidManifest) {
				t.Fatalf("Validate(%v) = %v, want ErrInvalidManifest", bad, err)
			}
		})
	}
}

func TestManifestAcceptsPartialConstraintFromFixHint(t *testing.T) {
	m, err := modding.ParseManifest([]byte("manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\ngameVersion: ^1.2\ndependencies:\n  b: ^1.2\n"))
	if err != nil {
		t.Fatalf("ParseManifest with ^1.2 constraint: %v", err)
	}
	if ok, err := modding.CheckConstraint("1.5.0", m.GameVersion); err != nil || !ok {
		t.Fatalf("CheckConstraint(1.5.0, %q) = %v, %v; want true, nil", m.GameVersion, ok, err)
	}
	if ok, err := modding.CheckConstraint("2.0.0", m.GameVersion); err != nil || ok {
		t.Fatalf("CheckConstraint(2.0.0, %q) = %v, %v; want false, nil", m.GameVersion, ok, err)
	}
}

func TestConstraintFixHintExamplesAllPassValidateConstraint(t *testing.T) {
	bad := modding.ModManifest{
		ManifestVersion: modding.MinManifestVersion,
		ID:              "a",
		Name:            "A",
		Version:         "1.0.0",
		Entry:           "mod.js",
		GameVersion:     "not-a-constraint",
	}
	err := bad.Validate()
	var le *loaderr.Error
	if !errors.As(err, &le) {
		t.Fatalf("Validate error = %v, want *loaderr.Error", err)
	}
	// Extract every double-quoted constraint example from the fix hint so the hint can never drift from ValidateConstraint.
	var examples []string
	rest := le.Fix
	for {
		_, after, ok := strings.Cut(rest, `"`)
		if !ok {
			break
		}
		ex, tail, ok := strings.Cut(after, `"`)
		if !ok {
			t.Fatalf("unbalanced quote in fix hint %q", le.Fix)
		}
		examples = append(examples, ex)
		rest = tail
	}
	wantExamples := []string{">=1.2.0", "^1.2", ">=1.0.0 <2.0.0"}
	if !slices.Equal(examples, wantExamples) {
		t.Fatalf("fix hint %q examples = %q, want %q", le.Fix, examples, wantExamples)
	}
	for _, ex := range examples {
		if err := modding.ValidateConstraint(ex); err != nil {
			t.Errorf("ValidateConstraint(%q) from fix hint failed: %v", ex, err)
		}
		good := bad
		good.GameVersion = ex
		if err := good.Validate(); err != nil {
			t.Errorf("ModManifest.Validate with GameVersion %q failed: %v", ex, err)
		}
	}
}

func TestSemVerPrereleaseValidationAndPrecedence(t *testing.T) {
	// SemVer 2.0.0 §9: numeric prerelease identifiers cannot have leading zeros,
	// identifiers must be non-empty, and only [0-9A-Za-z-] are allowed.
	invalid := []string{
		"1.0.0-01",
		"1.0.0-x.01",
		"1.0.0-a..b",
		"1.0.0-",
		"1.0.0-.a",
		"1.0.0-a.",
		"1.0.0-a_b",
		"1.0.0-x.+1",
	}
	for _, raw := range invalid {
		t.Run("refuse/"+raw, func(t *testing.T) {
			if got, err := modding.ParseSemVer(raw); err == nil {
				t.Fatalf("ParseSemVer(%q) = %+v, want error", raw, got)
			}
			if err := modding.ValidateConstraint(">=" + raw); err == nil {
				t.Fatalf("ValidateConstraint(%q) = nil, want error", ">="+raw)
			}
			m := modding.ModManifest{
				ManifestVersion: modding.MinManifestVersion,
				ID:              "a",
				Name:            "A",
				Version:         raw,
				Entry:           "mod.js",
			}
			if err := m.Validate(); !errors.Is(err, modding.ErrInvalidManifest) {
				t.Fatalf("ModManifest.Validate(version=%q) = %v, want ErrInvalidManifest", raw, err)
			}
		})
	}

	// SemVer 2.0.0 §11.4: identifiers with hyphens (like "-1") are alphanumeric and rank above all numeric identifiers.
	hyphen, err := modding.ParseSemVer("1.0.0-x.-1")
	if err != nil {
		t.Fatalf("ParseSemVer(1.0.0-x.-1): %v", err)
	}
	numeric, err := modding.ParseSemVer("1.0.0-x.5")
	if err != nil {
		t.Fatalf("ParseSemVer(1.0.0-x.5): %v", err)
	}
	if got := hyphen.Compare(numeric); got <= 0 {
		t.Fatalf("Compare(1.0.0-x.-1, 1.0.0-x.5) = %d, want > 0", got)
	}
	if got := numeric.Compare(hyphen); got >= 0 {
		t.Fatalf("Compare(1.0.0-x.5, 1.0.0-x.-1) = %d, want < 0", got)
	}
	if ok, err := modding.CheckConstraint("1.0.0-x.-1", ">=1.0.0-x.5"); err != nil || !ok {
		t.Fatalf("CheckConstraint(1.0.0-x.-1, >=1.0.0-x.5) = %v, %v; want true, nil", ok, err)
	}
	if ok, err := modding.CheckConstraint("1.0.0-x.5", ">=1.0.0-x.-1"); err != nil || ok {
		t.Fatalf("CheckConstraint(1.0.0-x.5, >=1.0.0-x.-1) = %v, %v; want false, nil", ok, err)
	}
}

func TestDiscoverFSRefusesLegacyJSONManifest(t *testing.T) {
	fsys := modFS(map[string]string{
		"old/mod.json": `{"id":"old","name":"Old","version":"1.0.0","entry":"mod.js"}`,
		"old/mod.js":   `export {};`,
	})
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr := modding.NewManager(cx, modding.ManagerOptions{})
		_, err := mgr.DiscoverFS(fsys)
		if !errors.Is(err, modding.ErrInvalidManifest) || !strings.Contains(err.Error(), "mod.yaml") {
			t.Errorf("DiscoverFS error = %v, want ErrInvalidManifest naming mod.yaml", err)
		}
		return core.View{}
	})
}

func TestReloadRefusesManifestReplacedByLegacyJSON(t *testing.T) {
	fsys := modFS(map[string]string{
		"swap/mod.yaml": "manifestVersion: 1\nid: swap\nname: Swap\nversion: 1.0.0\nentry: mod.js\n",
		"swap/mod.js":   `export {};`,
	})
	var mgr *modding.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{})
		if _, err := mgr.DiscoverFS(fsys); err != nil {
			t.Fatalf("DiscoverFS: %v", err)
		}
		if err := mgr.LoadAll(); err != nil {
			t.Fatalf("LoadAll: %v", err)
		}
		return core.View{}
	})
	delete(fsys, "swap/mod.yaml")
	fsys["swap/mod.json"] = &fstest.MapFile{Data: []byte(`{"id":"swap","name":"Swap","version":"2.0.0","entry":"mod.js"}`)}
	var err error
	app.Post(func() { err = mgr.Reload("swap") })
	app.Settle()
	if !errors.Is(err, modding.ErrInvalidManifest) || !strings.Contains(err.Error(), "mod.yaml") {
		t.Fatalf("Reload error = %v, want ErrInvalidManifest naming mod.yaml", err)
	}
}

func TestSampleModLoads(t *testing.T) {
	data, err := os.ReadFile("testdata/mods/boss-rush/mod.yaml")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := modding.ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest(sample): %v", err)
	}
	canonical, err := modding.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest(sample): %v", err)
	}
	if !bytes.Equal(canonical, data) {
		t.Fatalf("sample mod.yaml is not in canonical form:\n%s\nwant:\n%s", data, canonical)
	}

	var mgr *modding.Manager
	var spawns []modding.SpawnRequest
	var events []modding.Event
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{
			GameVersion: "0.2.0",
			Bridge: modding.GameBridge{
				OnSpawn: func(req modding.SpawnRequest) (string, error) {
					spawns = append(spawns, req)
					return "slime-1", nil
				},
				OnEmit: func(ev modding.Event) { events = append(events, ev) },
			},
		})
		got, err := mgr.Install("testdata/mods/boss-rush")
		if err != nil {
			t.Fatalf("Install(boss-rush): %v", err)
		}
		if !reflect.DeepEqual(got, manifest) {
			t.Fatalf("installed = %+v, want %+v", got, manifest)
		}
		packGot, err := mgr.Install("testdata/mods/slime-pack")
		if err != nil {
			t.Fatalf("Install(slime-pack): %v", err)
		}
		if packGot.ID != "com.example.slime-pack" {
			t.Fatalf("installed pack = %+v, want com.example.slime-pack", packGot)
		}
		if err := mgr.LoadAll(); err != nil {
			t.Fatalf("LoadAll: %v", err)
		}
		mgr.SetState("score", 4200)
		mgr.Step(5 * time.Second)
		mgr.Emit("boss_defeated", map[string]any{"name": "Slime King"})
		return core.View{Children: []core.Node{mgr.PanelsView("hud")}}
	})

	info, ok := mgr.Info(manifest.ID)
	if !ok || info.State != modding.StateLoaded {
		t.Fatalf("sample mod info = %+v, want loaded", info)
	}
	packInfo, ok := mgr.Info("com.example.slime-pack")
	if !ok || packInfo.State != modding.StateLoaded {
		t.Fatalf("sample content-only mod info = %+v, want loaded", packInfo)
	}
	if spriteData, err := fs.ReadFile(mgr.ContentFS(), "assets/sprites/slime.txt"); err != nil || strings.TrimSpace(string(spriteData)) != "slime-green-16x16" {
		t.Fatalf("ContentFS assets/sprites/slime.txt = (%q, %v), want slime-green-16x16", spriteData, err)
	}
	if l10nData, err := fs.ReadFile(mgr.ContentFS(), "l10n/en.yaml"); err != nil || !strings.Contains(string(l10nData), "Emerald Slime") {
		t.Fatalf("ContentFS l10n/en.yaml = (%q, %v), want Emerald Slime", l10nData, err)
	}
	if len(spawns) != 1 || spawns[0].Prefab != "slime" || spawns[0].X != 120 || spawns[0].Y != 80 {
		t.Fatalf("spawns = %+v, want one slime at (120, 80)", spawns)
	}
	if len(events) != 1 || events[0].Name != "boss-rush:wave" {
		t.Fatalf("events = %+v, want one boss-rush:wave", events)
	}
	logs := mgr.Logs(manifest.ID)
	if len(logs) != 1 || logs[0].Message != "Defeated: Slime King score: 4200" {
		t.Fatalf("logs = %+v", logs)
	}
	panels := mgr.Panels("hud")
	if len(panels) != 1 || panels[0].ID != "boss-timer" {
		t.Fatalf("hud panels = %+v, want boss-timer", panels)
	}
}

func FuzzParseManifest(f *testing.F) {
	f.Add([]byte(fullManifest))
	f.Add([]byte(fullV2Manifest))
	f.Add([]byte("manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\n"))
	f.Add([]byte("manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\ncapabilities: [readState, inventory.read, scene.edit]\n"))
	f.Add([]byte("manifestVersion: 2\nid: pack\nname: Pack\nversion: 1.0.0\ncontributes:\n  assets: assets\n"))
	f.Add([]byte("manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\ncapabilities: [ui, ui]\n"))
	f.Add([]byte(`{"id":"a","name":"A","version":"1.0.0","entry":"mod.js"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := modding.ParseManifest(data)
		if err != nil {
			if !errors.Is(err, modding.ErrInvalidManifest) && !errors.Is(err, modding.ErrCyclicDependency) {
				t.Fatalf("ParseManifest error %v does not wrap ErrInvalidManifest", err)
			}
			return
		}
		out, err := modding.EncodeManifest(m)
		if err != nil {
			t.Fatalf("EncodeManifest of a parsed manifest: %v", err)
		}
		back, err := modding.ParseManifest(out)
		if err != nil {
			t.Fatalf("ParseManifest(EncodeManifest) = %v\n%s", err, out)
		}
		if !reflect.DeepEqual(normalize(back), normalize(m)) {
			t.Fatalf("round trip = %+v, want %+v", back, m)
		}
		again, err := modding.EncodeManifest(back)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("second encode differs (%v):\n%s\nwant:\n%s", err, again, out)
		}
	})
}

// normalize treats empty maps and slices like missing ones and sorts platforms, since omitempty drops empties and EncodeManifest sorts platforms.
func normalize(m modding.ModManifest) modding.ModManifest {
	if len(m.Dependencies) == 0 {
		m.Dependencies = nil
	}
	if len(m.OptionalDependencies) == 0 {
		m.OptionalDependencies = nil
	}
	if len(m.Conflicts) == 0 {
		m.Conflicts = nil
	}
	if len(m.Platforms) == 0 {
		m.Platforms = nil
	} else {
		p := slices.Clone(m.Platforms)
		slices.Sort(p)
		m.Platforms = p
	}
	return m
}

func BenchmarkParseManifest(b *testing.B) {
	data := []byte(fullManifest)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := modding.ParseManifest(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeManifest(b *testing.B) {
	m, err := modding.ParseManifest([]byte(fullManifest))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := modding.EncodeManifest(m); err != nil {
			b.Fatal(err)
		}
	}
}
