package modding_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/modding"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"github.com/nsgo-dev/nsgo/tsx"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func modFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

func TestDiscoverAndDependencyOrdering(t *testing.T) {
	fsys := modFS(map[string]string{
		"addon/mod.yaml": `manifestVersion: 1
id: addon-mod
name: Addon Mod
version: "1.0.0"
author: Tester
entry: mod.tsx
gameVersion: ">=0.2.0"
capabilities: [emitEvent, readState, writeState]
dependencies:
  base-mod: "^1.0.0"
`,
		"addon/mod.tsx": `import { log, onStep, emit, getState, setState } from "game";
log("addon loaded");
onStep((dt) => {
	const order = (getState("stepOrder") as string[]) || [];
	order.push("addon");
	setState("stepOrder", order);
	emit("addon:tick", { dt });
});`,
		"base/mod.yaml": `manifestVersion: 1
id: base-mod
name: Base Mod
version: "1.2.0"
entry: mod.js
gameVersion: ">=0.1.0"
capabilities: [readState, writeState]
`,
		"base/mod.js": `import { log, onStep, onEvent, getState, setState } from "game";
log("base loaded");
let ticks = 0;
onStep((dt) => {
	const order = getState("stepOrder") || [];
	order.push("base");
	setState("stepOrder", order);
});
onEvent("addon:tick", () => {
	ticks++;
	setState("baseHeardTicks", ticks);
});`,
	})

	var mgr *modding.Manager
	var loadOrder []string
	var emitted []modding.Event

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{
			GameVersion: "0.2.0",
			Bridge: modding.GameBridge{
				OnLog: func(e modding.LogEntry) {
					if strings.HasSuffix(e.Message, "loaded") {
						loadOrder = append(loadOrder, e.ModID)
					}
				},
				OnEmit: func(ev modding.Event) {
					emitted = append(emitted, ev)
				},
			},
		})
		manifests, err := mgr.DiscoverFS(fsys)
		if err != nil {
			t.Fatalf("DiscoverFS failed: %v", err)
		}
		if len(manifests) != 2 || manifests[0].ID != "base-mod" || manifests[1].ID != "addon-mod" {
			t.Fatalf("unexpected topological order: %+v", manifests)
		}
		if err := mgr.LoadAll(); err != nil {
			t.Fatalf("LoadAll failed: %v", err)
		}
		return core.View{}
	})

	if !slices.Equal(loadOrder, []string{"base-mod", "addon-mod"}) {
		t.Fatalf("load order = %v, want [base-mod addon-mod]", loadOrder)
	}

	app.Post(func() {
		mgr.Step(time.Second / 60)
	})
	app.Settle()

	rawOrder, ok := mgr.GetState("stepOrder")
	if !ok {
		t.Fatal("stepOrder missing from game state")
	}
	stepOrder, ok := rawOrder.([]any)
	if !ok || len(stepOrder) != 2 || stepOrder[0] != "base" || stepOrder[1] != "addon" {
		t.Fatalf("stepOrder = %#v, want [base addon]", rawOrder)
	}
	heard, _ := mgr.GetState("baseHeardTicks")
	if heard != int64(1) && heard != float64(1) {
		t.Fatalf("baseHeardTicks = %#v, want 1", heard)
	}
	if len(emitted) != 1 || emitted[0].ModID != "addon-mod" || emitted[0].Name != "addon:tick" {
		t.Fatalf("emitted events = %+v", emitted)
	}
}

func TestDiscoverFromDirectoryOnDisk(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "hello")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `manifestVersion: 1
id: hello-disk
name: Hello Disk
version: "1.0.0"
entry: mod.ts
capabilities: [readState]
`
	script := `import { log, modInfo } from "game";
const info = modInfo();
log("disk:" + info.id + "@" + info.version);`
	if err := os.WriteFile(filepath.Join(modDir, "mod.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "mod.ts"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	var mgr *modding.Manager
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{})
		got, err := mgr.Discover(dir)
		if err != nil {
			t.Fatalf("Discover failed: %v", err)
		}
		if len(got) != 1 || got[0].ID != "hello-disk" {
			t.Fatalf("discovered = %+v", got)
		}
		if err := mgr.LoadAll(); err != nil {
			t.Fatalf("LoadAll failed: %v", err)
		}
		return core.View{}
	})

	logs := mgr.Logs("hello-disk")
	if len(logs) != 1 || logs[0].Message != "disk:hello-disk@1.0.0" {
		t.Fatalf("logs = %+v", logs)
	}
}

func TestDependencyRejectionCycleMissingAndVersionMismatch(t *testing.T) {
	cases := []struct {
		name    string
		gameVer string
		files   map[string]string
		wantErr error
	}{
		{
			name: "missing dependency",
			files: map[string]string{
				"a/mod.yaml": `manifestVersion: 1
id: mod-a
name: A
version: "1.0.0"
entry: mod.js
dependencies:
  missing-lib: "^1.0.0"
`,
				"a/mod.js": `export {};`,
			},
			wantErr: modding.ErrMissingDependency,
		},
		{
			name: "2-node cycle",
			files: map[string]string{
				"a/mod.yaml": `manifestVersion: 1
id: mod-a
name: A
version: "1.0.0"
entry: mod.js
dependencies:
  mod-b: "*"
`,
				"a/mod.js": `export {};`,
				"b/mod.yaml": `manifestVersion: 1
id: mod-b
name: B
version: "1.0.0"
entry: mod.js
dependencies:
  mod-a: "*"
`,
				"b/mod.js": `export {};`,
			},
			wantErr: modding.ErrCyclicDependency,
		},
		{
			name: "self dependency",
			files: map[string]string{
				"a/mod.yaml": `manifestVersion: 1
id: mod-a
name: A
version: "1.0.0"
entry: mod.js
dependencies:
  mod-a: "^1.0.0"
`,
				"a/mod.js": `export {};`,
			},
			wantErr: modding.ErrCyclicDependency,
		},
		{
			name: "dependency version mismatch",
			files: map[string]string{
				"base/mod.yaml": `manifestVersion: 1
id: base
name: Base
version: "1.2.0"
entry: mod.js
`,
				"base/mod.js": `export {};`,
				"ext/mod.yaml": `manifestVersion: 1
id: ext
name: Ext
version: "1.0.0"
entry: mod.js
dependencies:
  base: ">=2.0.0"
`,
				"ext/mod.js": `export {};`,
			},
			wantErr: modding.ErrVersionMismatch,
		},
		{
			name:    "game version mismatch",
			gameVer: "0.2.0",
			files: map[string]string{
				"a/mod.yaml": `manifestVersion: 1
id: mod-a
name: A
version: "1.0.0"
entry: mod.js
gameVersion: ">=1.0.0"
`,
				"a/mod.js": `export {};`,
			},
			wantErr: modding.ErrVersionMismatch,
		},
		{
			name: "duplicate id",
			files: map[string]string{
				"d1/mod.yaml": `manifestVersion: 1
id: dup
name: D1
version: "1.0.0"
entry: mod.js
`,
				"d1/mod.js": `export {};`,
				"d2/mod.yaml": `manifestVersion: 1
id: dup
name: D2
version: "1.0.0"
entry: mod.js
`,
				"d2/mod.js": `export {};`,
			},
			wantErr: modding.ErrDuplicateMod,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nsgotest.Start(t, func(cx *core.Scope) core.Node {
				mgr := modding.NewManager(cx, modding.ManagerOptions{GameVersion: tc.gameVer})
				_, err := mgr.DiscoverFS(modFS(tc.files))
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("DiscoverFS error = %v, want %v", err, tc.wantErr)
				}
				return core.View{}
			})
		})
	}
}

func TestGameAPIWithGameLoop(t *testing.T) {
	fsys := modFS(map[string]string{
		"spawner/mod.yaml": `manifestVersion: 1
id: spawner
name: Wave Spawner
version: "1.0.0"
entry: mod.tsx
capabilities: [readState, spawnEntity, emitEvent, audio, storage]
`,
		"spawner/mod.tsx": `import { onStep, registerHook, emit, spawn, getState, setState, playSound, log, storageGet, storageSet } from "game";

let elapsed = 0;
let wave = 0;

registerHook("reset", (payload) => {
	wave = 0;
	setState("wave", 0);
	log("reset:" + payload.reason);
});

onStep((dt) => {
	elapsed += dt;
	if (elapsed >= 0.049) {
		elapsed = 0;
		wave++;
		const mult = (getState("difficulty") as number) || 1;
		const id = spawn("slime", wave * 10, 20, { hp: 10 * mult });
		setState("wave", wave);
		storageSet("lastWave", String(wave));
		playSound("spawn_sfx", { volume: 0.75, rate: 1.1 });
		emit("wave:spawned", { wave, id });
		log("spawned", id, "saved=" + storageGet("lastWave"));
	}
});

export function currentWave(): number {
	return wave;
}`,
	})

	var mgr *modding.Manager
	var spawns []modding.SpawnRequest
	var events []modding.Event
	var sounds []modding.SoundRequest

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{
			StorageKey: "game.mods",
			Bridge: modding.GameBridge{
				OnSpawn: func(req modding.SpawnRequest) (string, error) {
					spawns = append(spawns, req)
					return "ent-" + req.Prefab, nil
				},
				OnEmit: func(ev modding.Event) {
					events = append(events, ev)
				},
				OnPlaySound: func(req modding.SoundRequest) error {
					sounds = append(sounds, req)
					return nil
				},
			},
		})
		mgr.SetState("difficulty", 2)
		if _, err := mgr.DiscoverFS(fsys); err != nil {
			t.Fatalf("DiscoverFS: %v", err)
		}
		if err := mgr.LoadAll(); err != nil {
			t.Fatalf("LoadAll: %v", err)
		}
		loop := &game.Loop{
			Rate:   60,
			Update: mgr.Step,
		}
		loop.Run(cx)
		return core.View{}
	})

	// Advance 100ms (6 steps at 60Hz -> 2 triggers at ~50ms and ~100ms).
	app.Advance(100 * time.Millisecond)

	if len(spawns) != 2 {
		t.Fatalf("spawns = %d (%+v), want 2", len(spawns), spawns)
	}
	if spawns[0].Prefab != "slime" || spawns[0].X != 10 || spawns[0].Y != 20 {
		t.Fatalf("unexpected first spawn: %+v", spawns[0])
	}
	if hp := spawns[0].Props["hp"]; hp != int64(20) && hp != float64(20) {
		t.Fatalf("spawn hp prop = %#v, want 20", hp)
	}
	if len(events) != 2 || events[0].Name != "wave:spawned" {
		t.Fatalf("events = %+v, want 2 wave:spawned", events)
	}
	if len(sounds) != 2 || sounds[0].Name != "spawn_sfx" || sounds[0].Volume != 0.75 {
		t.Fatalf("sounds = %+v", sounds)
	}
	logs := mgr.Logs("spawner")
	if len(logs) != 2 || !strings.Contains(logs[1].Message, "saved=2") {
		t.Fatalf("logs = %+v", logs)
	}

	// Test exported function Call + host Emit triggering registerHook("reset").
	var waveBefore any
	app.Post(func() {
		mgr.Call("spawner", "currentWave", nil, func(res any, err error) {
			if err != nil {
				t.Errorf("Call currentWave: %v", err)
			}
			waveBefore = res
		})
		mgr.Emit("reset", map[string]any{"reason": "new-run"})
	})
	app.Settle()

	if waveBefore != int64(2) && waveBefore != float64(2) {
		t.Fatalf("currentWave before reset = %#v, want 2", waveBefore)
	}
	if w, _ := mgr.GetState("wave"); w != int64(0) && w != float64(0) {
		t.Fatalf("wave after reset = %#v, want 0", w)
	}
}

func TestCapabilitySandboxingBlocksUnauthorizedCalls(t *testing.T) {
	fsys := modFS(map[string]string{
		"readonly/mod.yaml": `manifestVersion: 1
id: readonly
name: Read Only Mod
version: "1.0.0"
entry: mod.tsx
capabilities: [readState]
`,
		"readonly/mod.tsx": `import { log, getState, setState, spawn, emit, playSound, storageSet, mountPanel } from "game";

function check(name: string, fn: () => void) {
	try {
		fn();
		log(name + ":allowed");
	} catch (e: any) {
		log(name + ":" + (e.code || "ERR") + ":" + (e.permission || ""));
	}
}

log("read:" + getState("greeting"));
check("spawn", () => spawn("boss", 0, 0));
check("emit", () => emit("hack", {}));
check("setState", () => setState("greeting", "pwned"));
check("playSound", () => playSound("boom"));
check("storage", () => storageSet("k", "v"));
check("ui", () => mountPanel("p", () => null));
check("eval", () => { new Function("return 1")(); });`,

		"narrowed/mod.yaml": `manifestVersion: 1
id: narrowed
name: Requests Spawn But Host Denies
version: "1.0.0"
entry: mod.js
capabilities: [readState, spawnEntity]
`,
		"narrowed/mod.js": `import { spawn } from "game";
spawn("dragon", 1, 2);`,
	})

	grant := modding.CapReadState
	var mgr *modding.Manager
	var spawned int

	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{
			Grant: &grant,
			Bridge: modding.GameBridge{
				OnSpawn: func(req modding.SpawnRequest) (string, error) {
					spawned++
					return "never", nil
				},
			},
		})
		mgr.SetState("greeting", "hello")
		if _, err := mgr.DiscoverFS(fsys); err != nil {
			t.Fatalf("DiscoverFS: %v", err)
		}
		// LoadAll loads readonly cleanly and fails narrowed (which threw uncaught permission error).
		_ = mgr.LoadAll()
		return core.View{}
	})

	if spawned != 0 {
		t.Fatalf("unauthorized spawn reached host bridge (%d times)", spawned)
	}

	roInfo, _ := mgr.Info("readonly")
	if roInfo.State != modding.StateLoaded {
		t.Fatalf("readonly state = %s, err = %v", roInfo.State, roInfo.Error)
	}
	gotLogs := map[string]string{}
	for _, entry := range roInfo.Logs {
		k, v, _ := strings.Cut(entry.Message, ":")
		gotLogs[k] = v
	}
	want := map[string]string{
		"read":      "hello",
		"spawn":     "ERR_ACCESS_DENIED:spawnEntity",
		"emit":      "ERR_ACCESS_DENIED:emitEvent",
		"setState":  "ERR_ACCESS_DENIED:writeState",
		"playSound": "ERR_ACCESS_DENIED:audio",
		"storage":   "ERR_ACCESS_DENIED:storage",
		"ui":        "ERR_ACCESS_DENIED:ui",
	}
	for k, w := range want {
		if gotLogs[k] != w {
			t.Errorf("check %q = %q, want %q", k, gotLogs[k], w)
		}
	}
	if gotLogs["eval"] == "allowed" {
		t.Errorf("hardened sandbox allowed new Function()")
	}

	// Host Grant narrowed narrowed-mod's CapSpawnEntity away -> uncaught error marks it failed.
	narInfo, _ := mgr.Info("narrowed")
	if narInfo.State != modding.StateFailed || narInfo.Error == nil {
		t.Fatalf("narrowed state = %s, err = %v; want StateFailed", narInfo.State, narInfo.Error)
	}
	if !strings.Contains(narInfo.Error.Error(), "spawnEntity") {
		t.Fatalf("narrowed error = %v, want permission mention of spawnEntity", narInfo.Error)
	}
}

func TestFaultIsolationAcrossSyntaxLoadAndStepErrors(t *testing.T) {
	fsys := modFS(map[string]string{
		"syntax/mod.yaml": `manifestVersion: 1
id: bad-syntax
name: Bad Syntax
version: "1.0.0"
entry: mod.js
`,
		"syntax/mod.js": `function broken( { return ;;;`,

		"entry/mod.yaml": `manifestVersion: 1
id: bad-entry
name: Bad Entry
version: "1.0.0"
entry: mod.js
capabilities: [writeState]
`,
		"entry/mod.js": `import { log, onStep, setState } from "game";
log("before crash");
onStep(() => setState("badEntryStepped", true));
throw new Error("boom at init");`,

		"stepper/mod.yaml": `manifestVersion: 1
id: bad-step
name: Bad Step
version: "1.0.0"
entry: mod.js
capabilities: [writeState]
`,
		"stepper/mod.js": `import { onStep, setState } from "game";
let n = 0;
onStep(() => {
	n++;
	setState("badStepCount", n);
	if (n === 2) {
		throw new Error("boom on step 2");
	}
});`,

		"healthy/mod.yaml": `manifestVersion: 1
id: healthy
name: Healthy Mod
version: "1.0.0"
entry: mod.js
capabilities: [writeState]
`,
		"healthy/mod.js": `import { onStep, onEvent, setState } from "game";
let steps = 0;
let pings = 0;
onStep(() => {
	steps++;
	setState("healthySteps", steps);
});
onEvent("ping", () => {
	pings++;
	setState("healthyPings", pings);
});`,
	})

	var mgr *modding.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{})
		if _, err := mgr.DiscoverFS(fsys); err != nil {
			t.Fatalf("DiscoverFS: %v", err)
		}
		err := mgr.LoadAll()
		if err == nil {
			t.Fatal("LoadAll should report errors for failing mods")
		}
		return core.View{}
	})

	// bad-syntax and bad-entry failed during LoadAll; bad-step and healthy loaded.
	if info, _ := mgr.Info("bad-syntax"); info.State != modding.StateFailed || info.Error == nil {
		t.Fatalf("bad-syntax info = %+v, want StateFailed", info)
	}
	if info, _ := mgr.Info("bad-entry"); info.State != modding.StateFailed || !strings.Contains(info.Error.Error(), "boom at init") {
		t.Fatalf("bad-entry info = %+v, want StateFailed with boom at init", info)
	} else if len(info.Logs) != 1 || info.Logs[0].Message != "before crash" {
		t.Fatalf("bad-entry logs were not preserved on failure: %+v", info.Logs)
	}

	// Step 4 times: bad-step throws on step 2 and is isolated; healthy runs all 4 steps!
	app.Post(func() {
		for range 4 {
			mgr.Step(time.Second / 60)
		}
		mgr.Emit("ping", nil)
	})
	app.Settle()

	if _, stepped := mgr.GetState("badEntryStepped"); stepped {
		t.Fatal("failed entry mod's partial onStep hook should never have run")
	}
	if info, _ := mgr.Info("bad-step"); info.State != modding.StateFailed || !strings.Contains(info.Error.Error(), "boom on step 2") {
		t.Fatalf("bad-step info = %+v, want StateFailed with boom on step 2", info)
	}
	if c, _ := mgr.GetState("badStepCount"); c != int64(2) && c != float64(2) {
		t.Fatalf("badStepCount = %#v, want 2 (stopped after faulting)", c)
	}
	if info, _ := mgr.Info("healthy"); info.State != modding.StateLoaded {
		t.Fatalf("healthy state = %s, err = %v", info.State, info.Error)
	}
	if c, _ := mgr.GetState("healthySteps"); c != int64(4) && c != float64(4) {
		t.Fatalf("healthySteps = %#v, want 4", c)
	}
	if c, _ := mgr.GetState("healthyPings"); c != int64(1) && c != float64(1) {
		t.Fatalf("healthyPings = %#v, want 1", c)
	}
}

func TestDisableUnloadReloadAndStoragePersistence(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "counter")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `manifestVersion: 1
id: counter
name: Counter
version: "1.0.0"
entry: mod.js
capabilities: [readState, writeState]
`
	v1 := `import { onStep, getState, setState } from "game";
onStep(() => {
	setState("count", (getState("count") || 0) + 1);
});`
	v2 := `import { onStep, getState, setState } from "game";
onStep(() => {
	setState("count", (getState("count") || 0) + 10);
});`
	if err := os.WriteFile(filepath.Join(modDir, "mod.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "mod.js"), []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}

	var mgr *modding.Manager
	var rootCX *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		rootCX = cx
		mgr = modding.NewManager(cx, modding.ManagerOptions{StorageKey: "persist.mods"})
		if _, err := mgr.Discover(dir); err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if err := mgr.LoadAll(); err != nil {
			t.Fatalf("LoadAll: %v", err)
		}
		return core.View{}
	})

	// Step once with v1 (+1).
	app.Post(func() { mgr.Step(time.Second / 60) })
	app.Settle()
	if c, _ := mgr.GetState("count"); c != int64(1) && c != float64(1) {
		t.Fatalf("count after v1 step = %#v, want 1", c)
	}

	// Unload -> Step does nothing.
	app.Post(func() {
		if err := mgr.Unload("counter"); err != nil {
			t.Errorf("Unload: %v", err)
		}
		mgr.Step(time.Second / 60)
	})
	app.Settle()
	if c, _ := mgr.GetState("count"); c != int64(1) && c != float64(1) {
		t.Fatalf("count after unload+step = %#v, want 1", c)
	}

	// Update script on disk to v2 (+10) and Reload -> old hooks gone, v2 runs once (+10 => 11).
	if err := os.WriteFile(filepath.Join(modDir, "mod.js"), []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}
	app.Post(func() {
		if err := mgr.Reload("counter"); err != nil {
			t.Errorf("Reload: %v", err)
		}
		mgr.Step(time.Second / 60)
	})
	app.Settle()
	if c, _ := mgr.GetState("count"); c != int64(11) && c != float64(11) {
		t.Fatalf("count after reload+step = %#v, want 11", c)
	}

	// Disable -> persists to core.Storage, unloads hooks, and a fresh Manager honors it.
	app.Post(func() {
		if err := mgr.Disable("counter"); err != nil {
			t.Errorf("Disable: %v", err)
		}
		mgr.Step(time.Second / 60)
	})
	app.Settle()
	if c, _ := mgr.GetState("count"); c != int64(11) && c != float64(11) {
		t.Fatalf("count after disable+step = %#v, want 11", c)
	}
	if info, _ := mgr.Info("counter"); info.State != modding.StateDisabled || info.Enabled {
		t.Fatalf("disabled info = %+v", info)
	}

	var mgr2 *modding.Manager
	app.Post(func() {
		mgr2 = modding.NewManager(rootCX, modding.ManagerOptions{StorageKey: "persist.mods"})
		if _, err := mgr2.Discover(dir); err != nil {
			t.Errorf("mgr2 Discover: %v", err)
		}
		if err := mgr2.LoadAll(); err != nil {
			t.Errorf("mgr2 LoadAll: %v", err)
		}
	})
	app.Settle()

	if info, _ := mgr2.Info("counter"); info.State != modding.StateDisabled || info.Enabled {
		t.Fatalf("mgr2 did not restore persisted disabled state: %+v", info)
	}

	// Re-enable and load on mgr2.
	app.Post(func() {
		if err := mgr2.Enable("counter"); err != nil {
			t.Errorf("Enable: %v", err)
		}
		if err := mgr2.Load("counter"); err != nil {
			t.Errorf("Load after Enable: %v", err)
		}
		mgr2.Step(time.Second / 60)
	})
	app.Settle()
	if c, _ := mgr2.GetState("count"); c != int64(10) && c != float64(10) {
		t.Fatalf("mgr2 count after re-enable+step = %#v, want 10", c)
	}
}

func TestTSXUIPanelMountingAndCleanup(t *testing.T) {
	fsys := modFS(map[string]string{
		"hud/mod.yaml": `manifestVersion: 1
id: hud-mod
name: HUD Mod
version: "1.0.0"
entry: mod.tsx
capabilities: [ui]
`,
		"hud/mod.tsx": `import { signal, Text, View } from "nsgo";
import { onStep, mountPanel } from "game";

const ticks = signal(0);
onStep(() => ticks.set(ticks() + 1));

mountPanel("score-badge", () => (
	<View testID="mod-hud-root">
		<Text testID="mod-hud-text">{() => "Ticks: " + ticks()}</Text>
	</View>
), "hud");`,
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
		return core.View{Children: []core.Node{
			core.Text{Content: "Host HUD", TestID: "host-hud"},
			mgr.PanelsView("hud"),
		}}
	})

	if got := app.Find("id:mod-hud-text").Text; got != "Ticks: 0" {
		t.Fatalf("initial panel text = %q, want %q\n%s", got, "Ticks: 0", app.Dump())
	}

	app.Post(func() {
		mgr.Step(time.Second / 60)
		mgr.Step(time.Second / 60)
	})
	app.Settle()

	if got := app.Find("id:mod-hud-text").Text; got != "Ticks: 2" {
		t.Fatalf("updated panel text = %q, want %q", got, "Ticks: 2")
	}

	// Unloading the mod removes its mounted UI panel cleanly while host UI stays intact.
	app.Post(func() {
		if err := mgr.Unload("hud-mod"); err != nil {
			t.Errorf("Unload: %v", err)
		}
	})
	app.Settle()

	if app.Exists("id:mod-hud-text") {
		t.Fatalf("mod panel still rendered after unload:\n%s", app.Dump())
	}
	if app.Find("id:host-hud").Text != "Host HUD" {
		t.Fatal("host UI was disturbed by mod unload")
	}
}

func TestGameAPIDTSAndDeclarations(t *testing.T) {
	for _, want := range []string{
		`declare module "game"`,
		`export function registerHook`,
		`export function onStep`,
		`export function onEvent`,
		`export function emit`,
		`export function spawn`,
		`export function getState`,
		`export function setState`,
		`export function playSound`,
		`export function log`,
		`export function mountPanel`,
		`export function unmountPanel`,
		`export function storageGet`,
		`export function storageSet`,
		`export function storageRemove`,
	} {
		if !strings.Contains(modding.GameAPIDTS, want) {
			t.Errorf("GameAPIDTS missing %q", want)
		}
	}

	dst := filepath.Join(t.TempDir(), "types", "game.d.ts")
	if err := modding.WriteGameAPIDTS(dst); err != nil {
		t.Fatalf("WriteGameAPIDTS: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile game.d.ts: %v", err)
	}
	if string(data) != modding.GameAPIDTS {
		t.Fatal("written game.d.ts content mismatch")
	}

	decls := tsx.Declarations()
	if _, ok := decls["types/game.d.ts"]; !ok {
		t.Fatalf("tsx.Declarations() missing types/game.d.ts")
	}
}

func TestScopeDisposalAndSetGrantAndEventDepthGuard(t *testing.T) {
	fsys := modFS(map[string]string{
		"ping/mod.yaml": `manifestVersion: 1
id: ping
name: Ping
version: "1.0.0"
entry: mod.js
capabilities: [emitEvent, spawnEntity, writeState]
`,
		"ping/mod.js": `import { onEvent, emit, spawn, setState, log } from "game";
let hits = 0;
onEvent("bounce", () => {
	hits++;
	setState("pingHits", hits);
	try {
		emit("bounce", null);
	} catch (e) {
		log("depth-capped");
	}
});
export function trySpawn() {
	return spawn("goblin", 1, 2);
}`,
	})

	var shown *core.Signal[bool]
	var mgr *modding.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		shown = core.NewSignal(cx, true)
		return core.Show{
			When: shown.Get,
			Then: func(cx *core.Scope) core.Node {
				mgr = modding.NewManager(cx, modding.ManagerOptions{})
				if _, err := mgr.DiscoverFS(fsys); err != nil {
					t.Fatalf("DiscoverFS: %v", err)
				}
				// Narrow ping to emitEvent + writeState only (revoking spawnEntity per-mod).
				if err := mgr.SetGrant("ping", modding.CapEmitEvent|modding.CapWriteState); err != nil {
					t.Fatalf("SetGrant: %v", err)
				}
				if err := mgr.LoadAll(); err != nil {
					t.Fatalf("LoadAll: %v", err)
				}
				return core.View{}
			},
		}
	})

	// Per-mod SetGrant blocks trySpawn while allowing emit.
	var spawnErr error
	app.Post(func() {
		mgr.Call("ping", "trySpawn", nil, func(_ any, err error) {
			spawnErr = err
		})
		mgr.Emit("bounce", nil)
	})
	app.Settle()

	if spawnErr == nil || !strings.Contains(spawnErr.Error(), "spawnEntity") {
		t.Fatalf("trySpawn err = %v, want permission error for spawnEntity", spawnErr)
	}
	if hits, _ := mgr.GetState("pingHits"); hits != int64(16) && hits != float64(16) {
		t.Fatalf("pingHits = %#v, want 16 (bounded by event dispatch depth)", hits)
	}

	// Disposing the child scope automatically unloads all mods.
	app.Post(func() { shown.Set(false) })
	app.Settle()

	app.Post(func() {
		mgr.Emit("bounce", nil)
	})
	app.Settle()
	if hits, _ := mgr.GetState("pingHits"); hits != int64(16) && hits != float64(16) {
		t.Fatalf("pingHits changed after scope disposal: %#v", hits)
	}
}
