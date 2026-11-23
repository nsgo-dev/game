package modding_test

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/modding"
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
		ManifestVersion: modding.ManifestVersion,
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
		ManifestVersion: modding.ManifestVersion,
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
	m := modding.ModManifest{ManifestVersion: modding.ManifestVersion, ID: "tiny", Name: "Tiny", Version: "1.0.0", Entry: "mod.js"}
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
		"missing manifestVersion": {ID: "a", Name: "A", Version: "1.0.0", Entry: "mod.js"},
		"bad id":                  {ManifestVersion: 1, ID: "../a", Name: "A", Version: "1.0.0", Entry: "mod.js"},
		"undefined capability":    {ManifestVersion: 1, ID: "a", Name: "A", Version: "1.0.0", Entry: "mod.js", Capabilities: 1 << 30},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := modding.EncodeManifest(m); !errors.Is(err, modding.ErrInvalidManifest) {
				t.Fatalf("EncodeManifest error = %v, want ErrInvalidManifest", err)
			}
		})
	}
}

func TestParseManifestRefusesInvalidDocuments(t *testing.T) {
	const base = "manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\n"
	cases := map[string]struct {
		doc  string
		want string
	}{
		"empty":                   {"  \n", "empty manifest"},
		"unknown field":           {base + "homepage: https://example.com\n", "field homepage not found"},
		"misspelled field":        {strings.Replace(base, "entry:", "entrypoint:", 1), "entrypoint"},
		"missing manifestVersion": {strings.Replace(base, "manifestVersion: 1\n", "", 1), "unsupported manifestVersion 0"},
		"future manifestVersion":  {strings.Replace(base, "manifestVersion: 1", "manifestVersion: 2", 1), "unsupported manifestVersion 2"},
		"duplicate key":           {base + "name: B\n", "already defined"},
		"trailing document":       {base + "---\nid: b\n", "trailing YAML document"},
		"json manifest":           {`{"id":"a","name":"A","version":"1.0.0","entry":"mod.js"}`, "unsupported manifestVersion 0"},
		"not a mapping":           {"- a\n- b\n", "cannot unmarshal"},
		"unknown capability":      {base + "capabilities: [fly]\n", `unknown capability "fly"`},
		"non-canonical name":      {base + "capabilities: [read_state]\n", `unknown capability "read_state"`},
		"repeated capability":     {base + "capabilities: [ui, ui]\n", `capability "ui" listed twice`},
		"capabilities scalar":     {base + "capabilities: ui\n", "capabilities must be a sequence"},
		"capability not a name":   {base + "capabilities: [1]\n", "capability must be a name"},
		"bad dependency":          {base + "dependencies:\n  b: not-a-version\n", "constraint"},
		"bad version":             {strings.Replace(base, "1.0.0", "1.0", 1), "version"},
		"escaping entry":          {strings.Replace(base, "mod.js", "../mod.js", 1), "escapes"},
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
	if m.Capabilities != modding.CapNone {
		t.Fatalf("Capabilities = %v, want none", m.Capabilities)
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
		got, err := mgr.Discover("testdata/mods")
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if len(got) != 1 || !reflect.DeepEqual(got[0], manifest) {
			t.Fatalf("discovered = %+v, want [%+v]", got, manifest)
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
	f.Add([]byte("manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\n"))
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

// normalize treats an empty dependency map like a missing one, since omitempty drops both.
func normalize(m modding.ModManifest) modding.ModManifest {
	if len(m.Dependencies) == 0 {
		m.Dependencies = nil
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
