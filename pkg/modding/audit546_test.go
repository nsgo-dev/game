package modding_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/modding"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// startMods starts an app whose manager has discovered and loaded the mods in files.
func startMods(t *testing.T, files map[string]string) (*nsgotest.App, *modding.Manager) {
	t.Helper()
	var mgr *modding.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{})
		if _, err := mgr.DiscoverFS(modFS(files)); err != nil {
			t.Fatalf("DiscoverFS: %v", err)
		}
		if err := mgr.LoadAll(); err != nil {
			t.Fatalf("LoadAll: %v", err)
		}
		return core.View{}
	})
	app.Settle()
	return app, mgr
}

func TestCloseLeavesNoModLoaded(t *testing.T) {
	app, mgr := startMods(t, map[string]string{
		"a/mod.yaml": "manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\n",
		"a/mod.js":   `import { onStep } from "game"; onStep(() => {});`,
		"b/mod.yaml": "manifestVersion: 1\nid: b\nname: B\nversion: 1.0.0\nentry: mod.js\n",
		"b/mod.js":   `import { onStep } from "game"; onStep(() => {});`,
	})
	app.Post(func() {
		if err := mgr.Disable("b"); err != nil {
			t.Errorf("Disable: %v", err)
		}
		mgr.Close()
	})
	app.Settle()
	for id, want := range map[string]modding.ModState{"a": modding.StateDiscovered, "b": modding.StateDisabled} {
		info, ok := mgr.Info(id)
		if !ok || info.State != want {
			t.Fatalf("Info(%q).State after Close = %v, want %v", id, info.State, want)
		}
		if _, ok := mgr.API(id); ok {
			t.Fatalf("API(%q) still returned after Close", id)
		}
	}
}

func TestUnsubscribingAfterUnloadDoesNothing(t *testing.T) {
	app, mgr := startMods(t, map[string]string{
		"a/mod.yaml": "manifestVersion: 1\nid: a\nname: A\nversion: 1.0.0\nentry: mod.js\n",
		"a/mod.js":   `export const ok = true;`,
	})
	app.Post(func() {
		api, ok := mgr.API("a")
		if !ok {
			t.Fatal("mod a is not loaded")
		}
		offEvent, err := api.OnEvent("ping", func(any) {})
		if err != nil {
			t.Fatal(err)
		}
		offStep, err := api.OnStep(func(float64) {})
		if err != nil {
			t.Fatal(err)
		}
		if err := mgr.Unload("a"); err != nil {
			t.Fatal(err)
		}
		offEvent()
		offStep()
		offEvent()
	})
	app.Settle()
}

func TestSetGrantNarrowsALoadedMod(t *testing.T) {
	app, mgr := startMods(t, map[string]string{
		"ping/mod.yaml": "manifestVersion: 1\nid: ping\nname: Ping\nversion: 1.0.0\nentry: mod.js\ncapabilities: [spawnEntity, emitEvent]\n",
		"ping/mod.js": `import { spawn } from "game";
export function trySpawn() { return spawn("goblin", 1, 2); }`,
	})
	call := func() error {
		var callErr error
		app.Post(func() {
			mgr.Call("ping", "trySpawn", nil, func(_ any, err error) { callErr = err })
		})
		app.Settle()
		return callErr
	}
	if err := call(); err != nil {
		t.Fatalf("trySpawn before SetGrant: %v", err)
	}
	app.Post(func() {
		if err := mgr.SetGrant("ping", modding.CapEmitEvent); err != nil {
			t.Errorf("SetGrant: %v", err)
		}
	})
	app.Settle()
	if err := call(); err == nil || !strings.Contains(err.Error(), "spawnEntity") {
		t.Fatalf("trySpawn after SetGrant = %v, want a spawnEntity permission error", err)
	}
	info, _ := mgr.Info("ping")
	if info.State != modding.StateLoaded || info.Capabilities != modding.CapEmitEvent {
		t.Fatalf("Info after SetGrant = %v %v, want loaded with emitEvent only", info.State, info.Capabilities)
	}
}

// storageProbe exports helpers around the "nsgo" module's Storage so tests can check setItem, getItem, and removeItem directly.
const storageProbe = `import { Storage } from "nsgo";
import { getState, setState } from "game";
const loads = (getState("loads") || 0) + 1;
setState("loads", loads);
export function setItem(k, v) { return Storage.setItem(k, v); }
export function getItem(k) { return Storage.getItem(k); }
export function removeItem(k) { return Storage.removeItem(k); }`

func callMod(app *nsgotest.App, mgr *modding.Manager, id, fn string, args ...any) (any, error) {
	var res any
	var err error
	app.Post(func() {
		mgr.Call(id, fn, args, func(r any, e error) {
			res = r
			err = e
		})
	})
	app.Settle()
	return res, err
}

const storageDisabledMsg = "tsx: Storage is disabled for this sandbox"

func TestModWithoutStorageCannotPersistThroughNSGoStorage(t *testing.T) {
	t.Run("[readState, writeState]", func(t *testing.T) {
		app, mgr := startMods(t, map[string]string{
			"probe/mod.yaml": "manifestVersion: 1\nid: probe\nname: Probe\nversion: 1.0.0\nentry: mod.js\ncapabilities: [readState, writeState]\n",
			"probe/mod.js":   storageProbe,
		})
		for _, tc := range []struct {
			fn   string
			args []any
		}{
			{"setItem", []any{"a", ""}},
			{"setItem", []any{"score", "999"}},
			{"getItem", []any{"score"}},
			{"removeItem", []any{"score"}},
		} {
			if _, err := callMod(app, mgr, "probe", tc.fn, tc.args...); err == nil || !strings.Contains(err.Error(), storageDisabledMsg) {
				t.Fatalf("Storage.%s(%v) without CapStorage = %v, want error containing %q", tc.fn, tc.args, err, storageDisabledMsg)
			}
		}
	})

	t.Run("[readState, writeState, storage]", func(t *testing.T) {
		app, mgr := startMods(t, map[string]string{
			"probe/mod.yaml": "manifestVersion: 1\nid: probe\nname: Probe\nversion: 1.0.0\nentry: mod.js\ncapabilities: [readState, writeState, storage]\n",
			"probe/mod.js":   storageProbe,
		})
		if _, err := callMod(app, mgr, "probe", "setItem", "a", ""); err != nil {
			t.Fatalf("setItem(a, \"\") with CapStorage: %v", err)
		}
		if _, err := callMod(app, mgr, "probe", "setItem", "score", "999"); err != nil {
			t.Fatalf("setItem(score, 999) with CapStorage: %v", err)
		}
		if got, err := callMod(app, mgr, "probe", "getItem", "score"); err != nil || got != "999" {
			t.Fatalf("getItem(score) with CapStorage = (%v, %v), want (\"999\", nil)", got, err)
		}
		if _, err := callMod(app, mgr, "probe", "removeItem", "score"); err != nil {
			t.Fatalf("removeItem(score) with CapStorage: %v", err)
		}
		if got, err := callMod(app, mgr, "probe", "getItem", "score"); err != nil || got != nil {
			t.Fatalf("getItem(score) after removeItem = (%v, %v), want (nil, nil)", got, err)
		}
	})
}

func TestSetGrantRevokingStorageRebuildsTheSandbox(t *testing.T) {
	app, mgr := startMods(t, map[string]string{
		"probe/mod.yaml": "manifestVersion: 1\nid: probe\nname: Probe\nversion: 1.0.0\nentry: mod.js\ncapabilities: [readState, writeState, storage]\n",
		"probe/mod.js":   storageProbe,
	})
	if _, err := callMod(app, mgr, "probe", "setItem", "score", "saved"); err != nil {
		t.Fatalf("setItem before SetGrant: %v", err)
	}
	if got, err := callMod(app, mgr, "probe", "getItem", "score"); err != nil || got != "saved" {
		t.Fatalf("getItem before SetGrant = (%v, %v), want (\"saved\", nil)", got, err)
	}

	// Revoke CapStorage: the sandbox rebuilds with DisableStorage and rejects reads, writes (including 1-byte keys), and deletes.
	var grantErr error
	app.Post(func() { grantErr = mgr.SetGrant("probe", modding.CapReadState|modding.CapWriteState) })
	app.Settle()
	if grantErr != nil {
		t.Fatalf("SetGrant revoke: %v", grantErr)
	}
	if info, _ := mgr.Info("probe"); info.State != modding.StateLoaded {
		t.Fatalf("state after revoking storage = %v, want loaded", info.State)
	}
	for _, tc := range []struct {
		fn   string
		args []any
	}{
		{"setItem", []any{"a", ""}},
		{"getItem", []any{"score"}},
		{"removeItem", []any{"score"}},
	} {
		if _, err := callMod(app, mgr, "probe", tc.fn, tc.args...); err == nil || !strings.Contains(err.Error(), storageDisabledMsg) {
			t.Fatalf("Storage.%s(%v) after revoking CapStorage = %v, want error containing %q", tc.fn, tc.args, err, storageDisabledMsg)
		}
	}
	api, ok := mgr.API("probe")
	if !ok {
		t.Fatal("probe has no API after SetGrant reloaded it")
	}
	if _, _, err := api.StorageGet("score"); !errors.Is(err, modding.ErrPermission) {
		t.Fatalf("StorageGet after revoking storage = %v, want ErrPermission", err)
	}

	// Restore CapStorage: the sandbox rebuilds with Storage enabled again and still sees the partition's prior data.
	app.Post(func() {
		grantErr = mgr.SetGrant("probe", modding.CapReadState|modding.CapWriteState|modding.CapStorage)
	})
	app.Settle()
	if grantErr != nil {
		t.Fatalf("SetGrant restore: %v", grantErr)
	}
	if info, _ := mgr.Info("probe"); info.State != modding.StateLoaded {
		t.Fatalf("state after restoring storage = %v, want loaded", info.State)
	}
	if got, err := callMod(app, mgr, "probe", "getItem", "score"); err != nil || got != "saved" {
		t.Fatalf("getItem(score) after restoring CapStorage = (%v, %v), want (\"saved\", nil)", got, err)
	}
	if _, err := callMod(app, mgr, "probe", "setItem", "a", ""); err != nil {
		t.Fatalf("setItem(a, \"\") after restoring CapStorage: %v", err)
	}
	if _, err := callMod(app, mgr, "probe", "removeItem", "score"); err != nil {
		t.Fatalf("removeItem(score) after restoring CapStorage: %v", err)
	}
	if got, err := callMod(app, mgr, "probe", "getItem", "score"); err != nil || got != nil {
		t.Fatalf("getItem(score) after removeItem = (%v, %v), want (nil, nil)", got, err)
	}
	if loads, _ := mgr.GetState("loads"); loads != int64(3) && loads != float64(3) && loads != 3 {
		t.Fatalf("loads = %v (%T), want 3 (initial + revoke + restore)", loads, loads)
	}
}

func TestSetGrantRevokingUIRemovesPanels(t *testing.T) {
	app, mgr := startMods(t, map[string]string{
		"badge/mod.yaml": "manifestVersion: 1\nid: badge\nname: Badge\nversion: 1.0.0\nentry: mod.tsx\ncapabilities: [ui, emitEvent]\n",
		"badge/mod.tsx":  `import { Text } from "nsgo"; import { mountPanel } from "game"; mountPanel("score", () => <Text>score</Text>, "hud");`,
	})
	if panels := mgr.Panels("hud"); len(panels) != 1 || panels[0].ID != "score" {
		t.Fatalf("hud panels before SetGrant = %+v, want score", panels)
	}
	app.Post(func() {
		if err := mgr.SetGrant("badge", modding.CapEmitEvent); err != nil {
			t.Errorf("SetGrant: %v", err)
		}
	})
	app.Settle()
	if panels := mgr.Panels("hud"); len(panels) != 0 {
		t.Fatalf("hud panels after revoking ui = %+v, want none", panels)
	}
	if info, _ := mgr.Info("badge"); info.State != modding.StateLoaded {
		t.Fatalf("state after SetGrant = %v, want loaded", info.State)
	}
}
