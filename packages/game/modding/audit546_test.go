package modding_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/modding"
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

// storageProbe tries the "nsgo" module's Storage, which every sandbox exposes, and records the outcome in game state.
const storageProbe = `import { Storage } from "nsgo";
import { setState } from "game";
Storage.setItem("score", "999").then(
	() => setState("wrote", "yes"),
	() => setState("wrote", "no"),
);`

func TestModWithoutStorageCannotPersistThroughNSGoStorage(t *testing.T) {
	for _, tc := range []struct {
		caps, want string
	}{
		{"[writeState]", "no"},
		{"[writeState, storage]", "yes"},
	} {
		t.Run(tc.caps, func(t *testing.T) {
			_, mgr := startMods(t, map[string]string{
				"probe/mod.yaml": "manifestVersion: 1\nid: probe\nname: Probe\nversion: 1.0.0\nentry: mod.js\ncapabilities: " + tc.caps + "\n",
				"probe/mod.js":   storageProbe,
			})
			if got, _ := mgr.GetState("wrote"); got != tc.want {
				t.Fatalf("Storage.setItem from a mod with %s: wrote = %v, want %q", tc.caps, got, tc.want)
			}
		})
	}
}

func TestSetGrantRevokingStorageRebuildsTheSandbox(t *testing.T) {
	// Each load writes a new value: setting the value a key already holds is not a write.
	app, mgr := startMods(t, map[string]string{
		"probe/mod.yaml": "manifestVersion: 1\nid: probe\nname: Probe\nversion: 1.0.0\nentry: mod.js\ncapabilities: [readState, writeState, storage]\n",
		"probe/mod.js": `import { Storage } from "nsgo";
import { getState, setState } from "game";
const loads = (getState("loads") || 0) + 1;
setState("loads", loads);
Storage.setItem("score", "load " + loads).then(
	() => setState("wrote", "yes"),
	() => setState("wrote", "no"),
);`,
	})
	if got, _ := mgr.GetState("wrote"); got != "yes" {
		t.Fatalf("wrote = %v before SetGrant, want yes", got)
	}
	var grantErr error
	app.Post(func() { grantErr = mgr.SetGrant("probe", modding.CapReadState|modding.CapWriteState) })
	app.Settle()
	if grantErr != nil {
		t.Fatalf("SetGrant: %v", grantErr)
	}
	if got, _ := mgr.GetState("wrote"); got != "no" {
		t.Fatalf("wrote = %v after revoking storage, want no", got)
	}
	if info, _ := mgr.Info("probe"); info.State != modding.StateLoaded {
		t.Fatalf("state after SetGrant = %v, want loaded", info.State)
	}
	api, ok := mgr.API("probe")
	if !ok {
		t.Fatal("probe has no API after SetGrant reloaded it")
	}
	if _, _, err := api.StorageGet("score"); !errors.Is(err, modding.ErrPermission) {
		t.Fatalf("StorageGet after revoking storage = %v, want ErrPermission", err)
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
