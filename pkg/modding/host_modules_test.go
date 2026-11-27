package modding_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/modding"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func init() {
	// Pre-register test host module signatures before any test starts a TSX runtime.
	// Each test binds its own live closures per Manager via ManagerOptions.Modules.
	modding.RegisterModule(modding.ModModule{
		Name:       "mygame/inventory",
		Capability: "inventory.read",
		Doc:        "Read-only access to the player's inventory.",
		Funcs: map[string]any{
			"countItem": func(cx *core.Scope, item string) (int, error) { return 0, nil },
			"listItems": func(cx *core.Scope) ([]string, error) { return nil, nil },
			"sumItems":  func(cx *core.Scope, items ...string) (int, error) { return 0, nil },
			"banner":    func() string { return "" },
		},
	})
	modding.RegisterModule(modding.ModModule{
		Name:       "mygame/vault",
		Capability: "inventory.read",
		Doc:        "Inventory queries and mutations.",
		Funcs: map[string]any{
			"deposit": func(cx *core.Scope, item string, amount int) (int, error) { return 0, nil },
		},
	})
	modding.RegisterModule(modding.ModModule{
		Name:         "mygame/editor",
		Capabilities: []string{"editor.panel", "scene.read", "scene.edit"},
		Doc:          "Editor extension API.",
		Funcs: map[string]any{
			"registerPanel": func(cx *core.Scope, id string) error { return nil },
			"selection":     func(cx *core.Scope) ([]string, error) { return nil, nil },
			"deleteObject":  func(cx *core.Scope, id string) error { return nil },
		},
	})
}

func inventoryModule(items map[string]int, banner string, callers *[]string) modding.ModModule {
	return modding.ModModule{
		Name:       "mygame/inventory",
		Capability: "inventory.read",
		Doc:        "Read-only access to the player's inventory.",
		Funcs: map[string]any{
			"countItem": func(cx *core.Scope, item string) (int, error) {
				api, err := modding.Caller(cx)
				if err != nil {
					return 0, err
				}
				if callers != nil {
					*callers = append(*callers, api.Manifest().ID)
				}
				return items[item], nil
			},
			"listItems": func(cx *core.Scope) ([]string, error) {
				if _, err := modding.Caller(cx); err != nil {
					return nil, err
				}
				keys := make([]string, 0, len(items))
				for k := range items {
					keys = append(keys, k)
				}
				slices.Sort(keys)
				return keys, nil
			},
			"sumItems": func(cx *core.Scope, names ...string) (int, error) {
				if _, err := modding.Caller(cx); err != nil {
					return 0, err
				}
				total := 0
				for _, name := range names {
					total += items[name]
				}
				return total, nil
			},
			"banner": func() string {
				return banner
			},
		},
	}
}

func TestHostModuleGrantedAndUngrantedImport(t *testing.T) {
	fsys := modFS(map[string]string{
		"reader/mod.yaml": `manifestVersion: 1
id: mod.reader
name: Reader
version: 1.0.0
entry: mod.ts
capabilities: [inventory.read]
`,
		"reader/mod.ts": `import { modInfo } from "game";
import { countItem, listItems } from "mygame/inventory";

export function probe() {
	return {
		potions: countItem("potion"),
		items: listItems(),
		caps: modInfo().capabilities,
	};
}
`,
		"ungranted/mod.yaml": `manifestVersion: 1
id: mod.ungranted
name: Ungranted
version: 1.0.0
entry: mod.ts
capabilities: [readState]
`,
		"ungranted/mod.ts": `import { countItem } from "mygame/inventory";
export function probe() { return countItem("potion"); }
`,
	})

	items := map[string]int{"potion": 7, "gem": 3}
	var callers []string
	var mgr *modding.Manager
	var loadErr error
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{
			Capabilities: []modding.HostCapability{
				{Name: "inventory.read", Doc: "Read player inventory items"},
			},
			Modules: []modding.ModModule{
				inventoryModule(items, "v1", &callers),
			},
		})
		if _, err := mgr.DiscoverFS(fsys); err != nil {
			t.Fatalf("DiscoverFS: %v", err)
		}
		loadErr = mgr.LoadAll()
		return core.View{}
	})

	if loadErr == nil {
		t.Fatal("LoadAll succeeded, want error for mod.ungranted")
	}

	readerInfo, ok := mgr.Info("mod.reader")
	if !ok || readerInfo.State != modding.StateLoaded {
		t.Fatalf("mod.reader info = %+v, want loaded", readerInfo)
	}
	if !reflect.DeepEqual(readerInfo.HostCapabilities, []string{"inventory.read"}) {
		t.Fatalf("mod.reader HostCapabilities = %v, want [inventory.read]", readerInfo.HostCapabilities)
	}

	ungrantedInfo, ok := mgr.Info("mod.ungranted")
	if !ok || ungrantedInfo.State != modding.StateFailed || ungrantedInfo.Error == nil {
		t.Fatalf("mod.ungranted info = %+v, want failed at load", ungrantedInfo)
	}
	if !strings.Contains(ungrantedInfo.Error.Error(), "mygame/inventory") {
		t.Fatalf("mod.ungranted error = %q, want mention of mygame/inventory", ungrantedInfo.Error)
	}

	var res any
	var callErr error
	app.Post(func() {
		mgr.Call("mod.reader", "probe", nil, func(v any, err error) {
			res, callErr = v, err
		})
	})
	app.Settle()

	if callErr != nil {
		t.Fatalf("mod.reader probe failed: %v", callErr)
	}
	obj, _ := res.(map[string]any)
	if obj == nil {
		t.Fatalf("probe result = %#v, want map", res)
	}
	if got := obj["potions"]; got != int64(7) && got != float64(7) {
		t.Fatalf("potions = %#v, want 7", got)
	}
	if !reflect.DeepEqual(callers, []string{"mod.reader"}) {
		t.Fatalf("Caller recorded %v, want [mod.reader]", callers)
	}
	if gotCaps, ok := obj["caps"].([]string); !ok || !reflect.DeepEqual(gotCaps, []string{"inventory.read"}) {
		t.Fatalf("modInfo().capabilities = %#v, want []string{\"inventory.read\"}", obj["caps"])
	}

	// HostCaps declared on manager are retrievable for permission prompts.
	if declared := mgr.HostCapabilities(); len(declared) != 1 || declared[0].Name != "inventory.read" || declared[0].Doc != "Read player inventory items" {
		t.Fatalf("HostCapabilities() = %+v", declared)
	}
}

func TestHostModuleGrantHostWithholdsImportAtLoad(t *testing.T) {
	fsys := modFS(map[string]string{
		"reader/mod.yaml": `manifestVersion: 1
id: mod.reader
name: Reader
version: 1.0.0
entry: mod.ts
capabilities: [inventory.read]
`,
		"reader/mod.ts": `import { countItem } from "mygame/inventory";
export function probe() { return countItem("potion"); }
`,
	})

	var mgr *modding.Manager
	var loadErr error
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{
			Capabilities: []modding.HostCapability{
				{Name: "inventory.read", Doc: "Read player inventory items"},
			},
			Modules: []modding.ModModule{
				inventoryModule(map[string]int{"potion": 2}, "", nil),
			},
			GrantHost: []string{}, // non-nil empty: deny all host capabilities
		})
		if _, err := mgr.DiscoverFS(fsys); err != nil {
			t.Fatalf("DiscoverFS: %v", err)
		}
		loadErr = mgr.LoadAll()
		return core.View{}
	})

	if loadErr == nil {
		t.Fatal("LoadAll succeeded when GrantHost denied inventory.read")
	}
	info, _ := mgr.Info("mod.reader")
	if info.State != modding.StateFailed || len(info.HostCapabilities) != 0 {
		t.Fatalf("info = %+v, want failed with no effective host capabilities", info)
	}
}

func TestHostModuleSetHostGrantNarrowsLoadedMod(t *testing.T) {
	fsys := modFS(map[string]string{
		"trader/mod.yaml": `manifestVersion: 1
id: mod.trader
name: Trader
version: 1.0.0
entry: mod.ts
capabilities: [inventory.read, inventory.write]
`,
		"trader/mod.ts": `import { countItem } from "mygame/inventory";
import { deposit } from "mygame/vault";

export function readGems() {
	return countItem("gem");
}

export function addGems(n: number) {
	return deposit("gem", n);
}

export function catchAddGems(n: number) {
	try {
		deposit("gem", n);
		return { ok: true };
	} catch (e: any) {
		return { ok: false, code: e.code, permission: e.permission, resource: e.resource };
	}
}
`,
	})

	items := map[string]int{"gem": 4}
	var mgr *modding.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{
			Capabilities: []modding.HostCapability{
				{Name: "inventory.read", Doc: "Read inventory"},
				{Name: "inventory.write", Doc: "Modify inventory"},
			},
			Modules: []modding.ModModule{
				inventoryModule(items, "", nil),
				{
					Name:       "mygame/vault",
					Capability: "inventory.read",
					Funcs: map[string]any{
						"deposit": func(cx *core.Scope, item string, amount int) (int, error) {
							api, err := modding.Caller(cx)
							if err != nil {
								return 0, err
							}
							if err := api.RequireHost("inventory.write", "vault.deposit"); err != nil {
								return 0, err
							}
							items[item] += amount
							return items[item], nil
						},
					},
				},
			},
		})
		if _, err := mgr.DiscoverFS(fsys); err != nil {
			t.Fatalf("DiscoverFS: %v", err)
		}
		if err := mgr.LoadAll(); err != nil {
			t.Fatalf("LoadAll: %v", err)
		}
		return core.View{}
	})

	// Before narrowing: both read and write succeed.
	var res any
	var err error
	app.Post(func() {
		mgr.Call("mod.trader", "addGems", []any{3}, func(v any, e error) { res, err = v, e })
	})
	app.Settle()
	if err != nil || (res != int64(7) && res != float64(7)) {
		t.Fatalf("addGems before narrow = (%#v, %v), want (7, nil)", res, err)
	}

	// Narrow to inventory.read only: readGems still succeeds, deposit fails RequireHost("inventory.write").
	app.Post(func() {
		err = mgr.SetHostGrant("mod.trader", []string{"inventory.read"})
	})
	app.Settle()
	if err != nil {
		t.Fatalf("SetHostGrant: %v", err)
	}
	info, _ := mgr.Info("mod.trader")
	if !reflect.DeepEqual(info.HostCapabilities, []string{"inventory.read"}) {
		t.Fatalf("HostCapabilities after narrow = %v, want [inventory.read]", info.HostCapabilities)
	}

	app.Post(func() {
		mgr.Call("mod.trader", "readGems", nil, func(v any, e error) { res, err = v, e })
	})
	app.Settle()
	if err != nil || (res != int64(7) && res != float64(7)) {
		t.Fatalf("readGems after narrowing write = (%#v, %v), want (7, nil)", res, err)
	}

	var catchRes any
	app.Post(func() {
		mgr.Call("mod.trader", "catchAddGems", []any{2}, func(v any, e error) { catchRes, err = v, e })
	})
	app.Settle()
	if err != nil {
		t.Fatalf("catchAddGems threw: %v", err)
	}
	caught, _ := catchRes.(map[string]any)
	if caught == nil || caught["ok"] != false || caught["code"] != string(core.CodeAccessDenied) || caught["permission"] != "inventory.write" || caught["resource"] != "vault.deposit" {
		t.Fatalf("catchAddGems = %#v, want ACCESS_DENIED for inventory.write / vault.deposit", catchRes)
	}
	if items["gem"] != 7 {
		t.Fatalf("items[gem] = %d, want 7 (mutation must be blocked)", items["gem"])
	}

	// Also test Go-level RequireHost error unwrapping directly on *ModAPI.
	api, ok := mgr.API("mod.trader")
	if !ok {
		t.Fatal("API(mod.trader) missing")
	}
	reqErr := api.RequireHost("inventory.write", "vault.deposit")
	if !errors.Is(reqErr, modding.ErrPermission) {
		t.Fatalf("RequireHost err = %v, want ErrPermission", reqErr)
	}
	var pe *modding.PermissionError
	if !errors.As(reqErr, &pe) || pe.HostCapability != "inventory.write" || pe.Action != "vault.deposit" {
		t.Fatalf("PermissionError = %+v, want inventory.write / vault.deposit", pe)
	}
	var ce *core.Error
	if !errors.As(reqErr, &ce) || ce.Code != core.CodeAccessDenied || ce.Permission != "inventory.write" {
		t.Fatalf("core.Error = %+v, want ACCESS_DENIED for inventory.write", ce)
	}

	// Narrow to nothing (nil): even countItem on mygame/inventory fails immediately on the loaded mod.
	app.Post(func() {
		err = mgr.SetHostGrant("mod.trader", nil)
	})
	app.Settle()
	if err != nil {
		t.Fatalf("SetHostGrant(nil): %v", err)
	}
	var readErr error
	app.Post(func() {
		mgr.Call("mod.trader", "readGems", nil, func(_ any, e error) { readErr = e })
	})
	app.Settle()
	if readErr == nil || !strings.Contains(readErr.Error(), "inventory.read") {
		t.Fatalf("readGems after SetHostGrant(nil) err = %v, want permission error for inventory.read", readErr)
	}

	// Reloading after revoking inventory.read now blocks the import itself.
	var reloadErr error
	app.Post(func() {
		reloadErr = mgr.Reload("mod.trader")
	})
	app.Settle()
	if reloadErr == nil || !strings.Contains(reloadErr.Error(), "mygame/inventory") {
		t.Fatalf("Reload after revoking inventory.read err = %v, want import error", reloadErr)
	}
}

func TestHostModuleMultiCapabilityAndRequireHost(t *testing.T) {
	fsys := modFS(map[string]string{
		"reader/mod.yaml": `manifestVersion: 1
id: editor.reader
name: Scene Reader
version: 1.0.0
entry: mod.ts
capabilities: [scene.read]
`,
		"reader/mod.ts": `import { selection, deleteObject } from "mygame/editor";
export function getSel() { return selection(); }
export function tryDelete() { return deleteObject("hero"); }
`,
		"panel/mod.yaml": `manifestVersion: 1
id: editor.panel
name: Panel Tool
version: 1.0.0
entry: mod.ts
capabilities: [editor.panel]
`,
		"panel/mod.ts": `import { registerPanel, selection } from "mygame/editor";
registerPanel("stats");
export function trySel() { return selection(); }
`,
		"none/mod.yaml": `manifestVersion: 1
id: editor.none
name: No Caps
version: 1.0.0
entry: mod.ts
`,
		"none/mod.ts": `import { selection } from "mygame/editor";
export function trySel() { return selection(); }
`,
	})

	var panels []string
	var deleted []string
	var mgr *modding.Manager
	var loadErr error
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{
			Capabilities: []modding.HostCapability{
				{Name: "editor.panel", Doc: "Register editor panels"},
				{Name: "scene.read", Doc: "Read scene selection"},
				{Name: "scene.edit", Doc: "Edit scene objects"},
			},
			Modules: []modding.ModModule{
				{
					Name:         "mygame/editor",
					Capabilities: []string{"editor.panel", "scene.read", "scene.edit"},
					Funcs: map[string]any{
						"registerPanel": func(cx *core.Scope, id string) error {
							api, err := modding.Caller(cx)
							if err != nil {
								return err
							}
							if err := api.RequireHost("editor.panel", "editor.registerPanel"); err != nil {
								return err
							}
							panels = append(panels, id)
							return nil
						},
						"selection": func(cx *core.Scope) ([]string, error) {
							api, err := modding.Caller(cx)
							if err != nil {
								return nil, err
							}
							if err := api.RequireHost("scene.read", "editor.selection"); err != nil {
								return nil, err
							}
							return []string{"hero", "slime"}, nil
						},
						"deleteObject": func(cx *core.Scope, id string) error {
							api, err := modding.Caller(cx)
							if err != nil {
								return err
							}
							if err := api.RequireHost("scene.edit", "editor.deleteObject"); err != nil {
								return err
							}
							deleted = append(deleted, id)
							return nil
						},
					},
				},
			},
		})
		if _, err := mgr.DiscoverFS(fsys); err != nil {
			t.Fatalf("DiscoverFS: %v", err)
		}
		loadErr = mgr.LoadAll()
		return core.View{}
	})

	if loadErr == nil {
		t.Fatal("LoadAll succeeded, want editor.none to fail")
	}
	if info, _ := mgr.Info("editor.none"); info.State != modding.StateFailed {
		t.Fatalf("editor.none state = %v, want failed", info.State)
	}
	if info, _ := mgr.Info("editor.reader"); info.State != modding.StateLoaded {
		t.Fatalf("editor.reader state = %v, want loaded", info.State)
	}
	if info, _ := mgr.Info("editor.panel"); info.State != modding.StateLoaded {
		t.Fatalf("editor.panel state = %v, want loaded", info.State)
	}
	if !reflect.DeepEqual(panels, []string{"stats"}) {
		t.Fatalf("panels = %v, want [stats]", panels)
	}

	var sel any
	var selErr, delErr, panelSelErr error
	app.Post(func() {
		mgr.Call("editor.reader", "getSel", nil, func(v any, e error) { sel, selErr = v, e })
		mgr.Call("editor.reader", "tryDelete", nil, func(_ any, e error) { delErr = e })
		mgr.Call("editor.panel", "trySel", nil, func(_ any, e error) { panelSelErr = e })
	})
	app.Settle()

	if selErr != nil {
		t.Fatalf("getSel failed: %v", selErr)
	}
	if got, ok := sel.([]any); !ok || len(got) != 2 || got[0] != "hero" || got[1] != "slime" {
		t.Fatalf("getSel = %#v, want [hero slime]", sel)
	}
	if delErr == nil || !strings.Contains(delErr.Error(), "scene.edit") {
		t.Fatalf("tryDelete err = %v, want permission error for scene.edit", delErr)
	}
	if len(deleted) != 0 {
		t.Fatalf("deleted = %v, want empty", deleted)
	}
	if panelSelErr == nil || !strings.Contains(panelSelErr.Error(), "scene.read") {
		t.Fatalf("editor.panel trySel err = %v, want permission error for scene.read", panelSelErr)
	}
}

func TestUnknownHostCapabilityFailsOneModOnly(t *testing.T) {
	fsys := modFS(map[string]string{
		"bad/mod.yaml": `manifestVersion: 1
id: mod.bad
name: Bad Cap Mod
version: 1.0.0
entry: mod.ts
capabilities: [readState, mystery.portal]
`,
		"bad/mod.ts": `export function hi() { return "bad"; }`,
		"good/mod.yaml": `manifestVersion: 1
id: mod.good
name: Good Mod
version: 1.0.0
entry: mod.ts
capabilities: [inventory.read]
`,
		"good/mod.ts": `import { countItem } from "mygame/inventory";
export function gems() { return countItem("gem"); }
`,
	})

	var mgr *modding.Manager
	var loadErr error
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{
			Capabilities: []modding.HostCapability{
				{Name: "inventory.read", Doc: "Read inventory"},
			},
			Modules: []modding.ModModule{
				inventoryModule(map[string]int{"gem": 9}, "", nil),
			},
		})
		manifests, err := mgr.DiscoverFS(fsys)
		if err != nil {
			t.Fatalf("DiscoverFS: %v", err)
		}
		if len(manifests) != 2 {
			t.Fatalf("discovered %d mods, want 2", len(manifests))
		}
		loadErr = mgr.LoadAll()
		return core.View{}
	})

	if !errors.Is(loadErr, modding.ErrUnknownCapability) {
		t.Fatalf("LoadAll err = %v, want ErrUnknownCapability", loadErr)
	}

	badInfo, _ := mgr.Info("mod.bad")
	if badInfo.State != modding.StateFailed || !errors.Is(badInfo.Error, modding.ErrUnknownCapability) || !strings.Contains(badInfo.Error.Error(), "mystery.portal") {
		t.Fatalf("mod.bad info = %+v, want StateFailed with ErrUnknownCapability for mystery.portal", badInfo)
	}

	goodInfo, _ := mgr.Info("mod.good")
	if goodInfo.State != modding.StateLoaded {
		t.Fatalf("mod.good info = %+v, want StateLoaded", goodInfo)
	}

	var res any
	var callErr error
	app.Post(func() {
		mgr.Call("mod.good", "gems", nil, func(v any, e error) { res, callErr = v, e })
	})
	app.Settle()
	if callErr != nil || (res != int64(9) && res != float64(9)) {
		t.Fatalf("mod.good gems = (%#v, %v), want (9, nil)", res, callErr)
	}
}

func TestHostModuleRegistrationValidationAndDuplicates(t *testing.T) {
	validCap := []modding.HostCapability{{Name: "inventory.read", Doc: "Read inventory"}}
	validMod := inventoryModule(map[string]int{}, "", nil)

	cases := map[string]struct {
		opts modding.ManagerOptions
		want string
	}{
		"duplicate module name": {
			opts: modding.ManagerOptions{
				Capabilities: validCap,
				Modules:      []modding.ModModule{validMod, validMod},
			},
			want: "duplicate host module",
		},
		"collides with game": {
			opts: modding.ManagerOptions{
				Capabilities: validCap,
				Modules: []modding.ModModule{{
					Name:       modding.GameModule,
					Capability: "inventory.read",
					Funcs:      map[string]any{"x": func(*core.Scope) {}},
				}},
			},
			want: `collides with built-in "game"`,
		},
		"reserved nsgo prefix": {
			opts: modding.ManagerOptions{
				Capabilities: validCap,
				Modules: []modding.ModModule{{
					Name:       "nsgo/custom",
					Capability: "inventory.read",
					Funcs:      map[string]any{"x": func(*core.Scope) {}},
				}},
			},
			want: "reserved nsgo prefix",
		},
		"escaping module name": {
			opts: modding.ManagerOptions{
				Capabilities: validCap,
				Modules: []modding.ModModule{{
					Name:       "../evil",
					Capability: "inventory.read",
					Funcs:      map[string]any{"x": func(*core.Scope) {}},
				}},
			},
			want: "invalid host module name",
		},
		"missing capability": {
			opts: modding.ManagerOptions{
				Capabilities: validCap,
				Modules: []modding.ModModule{{
					Name:  "mygame/inventory",
					Funcs: validMod.Funcs,
				}},
			},
			want: "must declare a required capability",
		},
		"undeclared capability": {
			opts: modding.ManagerOptions{
				Capabilities: validCap,
				Modules: []modding.ModModule{{
					Name:       "mygame/inventory",
					Capability: "inventory.write",
					Funcs:      validMod.Funcs,
				}},
			},
			want: `undeclared capability "inventory.write"`,
		},
		"both capability and capabilities": {
			opts: modding.ManagerOptions{
				Capabilities: validCap,
				Modules: []modding.ModModule{{
					Name:         "mygame/inventory",
					Capability:   "inventory.read",
					Capabilities: []string{"inventory.read"},
					Funcs:        validMod.Funcs,
				}},
			},
			want: "not both",
		},
		"invalid host capability name": {
			opts: modding.ManagerOptions{
				Capabilities: []modding.HostCapability{{Name: "nodot"}},
			},
			want: "invalid host capability name",
		},
		"duplicate host capability": {
			opts: modding.ManagerOptions{
				Capabilities: []modding.HostCapability{{Name: "inventory.read"}, {Name: "inventory.read"}},
			},
			want: "duplicate host capability",
		},
		"empty funcs": {
			opts: modding.ManagerOptions{
				Capabilities: validCap,
				Modules: []modding.ModModule{{
					Name:       "mygame/inventory",
					Capability: "inventory.read",
				}},
			},
			want: "at least one function",
		},
		"non-func export": {
			opts: modding.ManagerOptions{
				Capabilities: validCap,
				Modules: []modding.ModModule{{
					Name:       "mygame/inventory",
					Capability: "inventory.read",
					Funcs:      map[string]any{"bad": 42},
				}},
			},
			want: "is not a function",
		},
		"mismatched signature with previous registration": {
			opts: modding.ManagerOptions{
				Capabilities: validCap,
				Modules: []modding.ModModule{{
					Name:       "mygame/inventory",
					Capability: "inventory.read",
					Funcs: map[string]any{
						"countItem": func(cx *core.Scope, item int) (int, error) { return 0, nil },
						"listItems": func(cx *core.Scope) ([]string, error) { return nil, nil },
						"sumItems":  func(cx *core.Scope, items ...string) (int, error) { return 0, nil },
						"banner":    func() string { return "" },
					},
				}},
			},
			want: "signature differs",
		},
	}

	root := core.NewRoot(core.RootOptions{})
	defer root.Dispose()
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("NewManager did not panic")
				}
				if !strings.Contains(toString(r), tc.want) {
					t.Fatalf("panic = %q, want it to contain %q", r, tc.want)
				}
			}()
			modding.NewManager(root, tc.opts)
		})
	}

	t.Run("duplicate RegisterModule panics", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil || !strings.Contains(toString(r), "duplicate host module registration") {
				t.Fatalf("RegisterModule duplicate panic = %v, want duplicate error", r)
			}
		}()
		modding.RegisterModule(validMod)
	})
}

func toString(v any) string {
	if err, ok := v.(error); ok {
		return err.Error()
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func TestHostModuleManagerIsolationAndVariadicAndNoScope(t *testing.T) {
	fsys := modFS(map[string]string{
		"bag/mod.yaml": `manifestVersion: 1
id: mod.bag
name: Bag
version: 1.0.0
entry: mod.ts
capabilities: [inventory.read]
`,
		"bag/mod.ts": `import { sumItems, banner } from "mygame/inventory";
export function report() {
	return banner() + ":" + sumItems("gold", "silver");
}
`,
	})

	var mgr1, mgr2 *modding.Manager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		caps := []modding.HostCapability{{Name: "inventory.read", Doc: "Read inventory"}}
		mgr1 = modding.NewManager(cx, modding.ManagerOptions{
			Capabilities: caps,
			Modules:      []modding.ModModule{inventoryModule(map[string]int{"gold": 10, "silver": 5}, "world1", nil)},
		})
		mgr2 = modding.NewManager(cx, modding.ManagerOptions{
			Capabilities: caps,
			Modules:      []modding.ModModule{inventoryModule(map[string]int{"gold": 100, "silver": 23}, "world2", nil)},
		})
		if _, err := mgr1.DiscoverFS(fsys); err != nil {
			t.Fatalf("mgr1 DiscoverFS: %v", err)
		}
		if _, err := mgr2.DiscoverFS(fsys); err != nil {
			t.Fatalf("mgr2 DiscoverFS: %v", err)
		}
		if err := mgr1.LoadAll(); err != nil {
			t.Fatalf("mgr1 LoadAll: %v", err)
		}
		if err := mgr2.LoadAll(); err != nil {
			t.Fatalf("mgr2 LoadAll: %v", err)
		}
		return core.View{}
	})

	var r1, r2 any
	var e1, e2 error
	app.Post(func() {
		mgr1.Call("mod.bag", "report", nil, func(v any, e error) { r1, e1 = v, e })
		mgr2.Call("mod.bag", "report", nil, func(v any, e error) { r2, e2 = v, e })
	})
	app.Settle()

	if e1 != nil || r1 != "world1:15" {
		t.Fatalf("mgr1 report = (%#v, %v), want (world1:15, nil)", r1, e1)
	}
	if e2 != nil || r2 != "world2:123" {
		t.Fatalf("mgr2 report = (%#v, %v), want (world2:123, nil)", r2, e2)
	}
}

func TestWriteModAPIDTS(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mod-types")
	opts := modding.ManagerOptions{
		Capabilities: []modding.HostCapability{
			{Name: "inventory.read", Doc: "Read inventory"},
			{Name: "editor.panel", Doc: "Register panels"},
			{Name: "scene.read", Doc: "Read scene"},
			{Name: "scene.edit", Doc: "Edit scene"},
		},
		Modules: []modding.ModModule{
			inventoryModule(map[string]int{}, "", nil),
			{
				Name:         "mygame/editor",
				Capabilities: []string{"editor.panel", "scene.read", "scene.edit"},
				Doc:          "Editor extension API.",
				Funcs: map[string]any{
					"registerPanel": func(cx *core.Scope, id string) error { return nil },
					"selection":     func(cx *core.Scope) ([]string, error) { return nil, nil },
					"deleteObject":  func(cx *core.Scope, id string) error { return nil },
				},
			},
		},
	}

	if err := modding.WriteModAPIDTS(dir, opts); err != nil {
		t.Fatalf("WriteModAPIDTS: %v", err)
	}

	gameDTS, err := os.ReadFile(filepath.Join(dir, "game.d.ts"))
	if err != nil {
		t.Fatalf("read game.d.ts: %v", err)
	}
	if string(gameDTS) != modding.GameAPIDTS {
		t.Fatal("game.d.ts differs from GameAPIDTS")
	}

	invDTS, err := os.ReadFile(filepath.Join(dir, "mygame", "inventory.d.ts"))
	if err != nil {
		t.Fatalf("read mygame/inventory.d.ts: %v", err)
	}
	for _, want := range []string{
		"Read-only access to the player's inventory.",
		"Requires capability: inventory.read",
		"export declare function countItem(arg0: string): number;",
		"export declare function listItems(): (string)[];",
		"export declare function sumItems(...arg0: (string)[]): number;",
		"export declare function banner(): string;",
	} {
		if !strings.Contains(string(invDTS), want) {
			t.Errorf("mygame/inventory.d.ts missing %q\ngot:\n%s", want, invDTS)
		}
	}

	edDTS, err := os.ReadFile(filepath.Join(dir, "mygame", "editor.d.ts"))
	if err != nil {
		t.Fatalf("read mygame/editor.d.ts: %v", err)
	}
	for _, want := range []string{
		"Editor extension API.",
		"Requires one of capabilities: editor.panel, scene.read, scene.edit",
		"export declare function registerPanel(arg0: string): void;",
		"export declare function selection(): (string)[];",
		"export declare function deleteObject(arg0: string): void;",
	} {
		if !strings.Contains(string(edDTS), want) {
			t.Errorf("mygame/editor.d.ts missing %q\ngot:\n%s", want, edDTS)
		}
	}

	if err := modding.WriteModAPIDTS("", opts); err == nil {
		t.Fatal("WriteModAPIDTS accepted empty dir")
	}
	if err := modding.WriteModAPIDTS(dir, modding.ManagerOptions{
		Modules: []modding.ModModule{{Name: "game", Funcs: map[string]any{"x": func() {}}}},
	}); err == nil {
		t.Fatal("WriteModAPIDTS accepted invalid options")
	}
}

func TestSampleHostModuleModInTestdata(t *testing.T) {
	items := map[string]int{"gem": 12, "relic": 2}
	var events []modding.Event
	var mgr *modding.Manager

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = modding.NewManager(cx, modding.ManagerOptions{
			GameVersion: "0.2.0",
			Capabilities: []modding.HostCapability{
				{Name: "inventory.read", Doc: "Read inventory counts"},
			},
			Modules: []modding.ModModule{
				inventoryModule(items, "", nil),
			},
			Bridge: modding.GameBridge{
				OnEmit: func(ev modding.Event) { events = append(events, ev) },
			},
		})
		discovered, err := mgr.Discover("testdata/mods")
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if len(discovered) < 2 {
			t.Fatalf("discovered %d mods, want at least 2 (boss-rush + loot-tracker)", len(discovered))
		}
		if err := mgr.LoadAll(); err != nil {
			t.Fatalf("LoadAll: %v", err)
		}
		mgr.SetState("zone", "cavern")
		mgr.Emit("loot_picked_up", map[string]any{"item": "gem"})
		return core.View{}
	})

	info, ok := mgr.Info("com.example.loot-tracker")
	if !ok || info.State != modding.StateLoaded {
		t.Fatalf("loot-tracker info = %+v, want loaded", info)
	}
	if !reflect.DeepEqual(info.HostCapabilities, []string{"inventory.read"}) {
		t.Fatalf("loot-tracker HostCapabilities = %v, want [inventory.read]", info.HostCapabilities)
	}
	logs := mgr.Logs("com.example.loot-tracker")
	if len(logs) != 1 || logs[0].Message != "Picked up gem total: 12 zone: cavern" {
		t.Fatalf("loot-tracker logs = %+v", logs)
	}
	if len(events) != 1 || events[0].Name != "loot-tracker:updated" {
		t.Fatalf("events = %+v, want loot-tracker:updated", events)
	}

	var summary any
	var callErr error
	app.Post(func() {
		mgr.Call("com.example.loot-tracker", "summarize", nil, func(v any, e error) { summary, callErr = v, e })
	})
	app.Settle()
	if callErr != nil {
		t.Fatalf("summarize: %v", callErr)
	}
	sumObj, _ := summary.(map[string]any)
	if sumObj == nil || (sumObj["gems"] != int64(12) && sumObj["gems"] != float64(12)) {
		t.Fatalf("summarize = %#v, want gems=12", summary)
	}

	// Without inventory.read declared, loot-tracker fails alone while boss-rush still loads.
	var bareMgr *modding.Manager
	var bareLoadErr error
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		bareMgr = modding.NewManager(cx, modding.ManagerOptions{GameVersion: "0.2.0"})
		if _, err := bareMgr.Discover("testdata/mods"); err != nil {
			t.Fatalf("bare Discover: %v", err)
		}
		bareLoadErr = bareMgr.LoadAll()
		return core.View{}
	})
	if !errors.Is(bareLoadErr, modding.ErrUnknownCapability) {
		t.Fatalf("bare LoadAll err = %v, want ErrUnknownCapability", bareLoadErr)
	}
	if bossInfo, _ := bareMgr.Info("com.example.boss-rush"); bossInfo.State != modding.StateLoaded {
		t.Fatalf("boss-rush state without host caps = %v, want loaded", bossInfo.State)
	}
	if lootInfo, _ := bareMgr.Info("com.example.loot-tracker"); lootInfo.State != modding.StateFailed {
		t.Fatalf("loot-tracker state without host caps = %v, want failed", lootInfo.State)
	}
}
