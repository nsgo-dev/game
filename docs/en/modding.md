# modding

`game/modding` loads and runs capability-sandboxed TSX/JS game mods on top of NSGo's `tsx` runtime, with `mod.yaml` manifest validation, topological dependency resolution, fault isolation, persisted enable/disable state, and a `"game"` module API.

```go
import "github.com/nsgo-dev/game/pkg/modding"
```

## Mods, plugins and packs (#1268)

The engine has three ways to extend a game, and they are not interchangeable:

| | Editor (Go) plugin | Script mod | Library pack |
| --- | --- | --- | --- |
| Written in | Go | TSX | YAML and media |
| Who writes it | the game's developers, third-party tool authors | players | anyone |
| When it loads | compiled into the editor binary | at run time, with hot reload (`Manager.Reload`) | installed into the project |
| Trust | full | sandboxed, capability-gated | carries no code |
| Platforms | all, iOS and the web included | native; the web only with `ManagerOptions.WebUnisolated` | all |
| Extends the editor | yes | no (today) | turns on built-in plugins only |

An editor plugin is an `editor.Plugin` the editor binary registers with `Editor.RegisterPlugin` and a project turns on by name under `editor.plugins` in `game.yaml`; see [Editor plugins](editor.md#editor-plugins-492-898). A script mod is a `mod.yaml` manifest plus a TSX entry script that `modding.Manager` runs in a sandbox limited to the capabilities the manifest asks for and the host grants, as this page describes. A library pack is a set of files `library.Install` writes into the project and records in `game.yaml`; see [Asset and Plugin Library](library.md). A pack may not hold Go source or module files (`.go`, `go.mod` and the like are refused), so a plugin pack only turns on editor plugins the editor binary was already built with — it never ships their code.

Which one do you need?

- A new panel, inspector, viewport tool or 3D gizmo in the editor, or component types the editor checks scenes against: an editor plugin — see [Editor plugins](editor.md#editor-plugins-492-898).
- Let players change the shipped game, within limits the game sets: a script mod, below.
- Share sprites, scenes, sounds or a starter template: a library pack — see [Asset and Plugin Library](library.md).

A Go plugin is compiled into the binary rather than loaded at run time because Go has no portable way to load code on every platform — iOS and the web cannot load native code at run time — so a plugin ships inside the editor command it extends.

## Mod manifest (`mod.yaml`) and capabilities

Each mod lives in its own directory with a `mod.yaml` (`ModManifest`) and an entry script (`mod.tsx` or `mod.js`), or one or more `contributes` directories for a content-only version 2 mod. The sample mods in [`pkg/modding/testdata/mods/boss-rush/`](../../pkg/modding/testdata/mods/boss-rush/) (v1 script mod) and [`pkg/modding/testdata/mods/slime-pack/`](../../pkg/modding/testdata/mods/slime-pack/) (v2 content-only mod) are loaded by the package's tests. Published JSON Schemas live at `https://game.nsgo.dev/schemas/mod.v1.json` and `https://game.nsgo.dev/schemas/mod.v2.json`.

```yaml
manifestVersion: 2
id: com.example.boss-rush
name: Boss Rush
version: 1.3.0
author: Example Studio
description: Spawns a slime every five seconds and shows a HUD badge while the rush runs.
entry: mod.tsx
gameVersion: '>=0.2.0'
capabilities: [readState, spawnEntity, emitEvent, ui]
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
```

| Field | Required | Meaning |
| --- | --- | --- |
| `manifestVersion` | yes | Schema version of the file: `1` (`modding.MinManifestVersion`) or `2` (`modding.ManifestVersion`) |
| `id` | yes | Unique ID of letters, digits, `.`, `-` and `_`, such as `com.example.boss-rush` |
| `name` | yes | Human-readable name |
| `version` | yes | The mod's own semantic version (`MAJOR.MINOR.PATCH[-prerelease][+build]`, optional leading `v`) |
| `author`, `description` | no | Free text |
| `entry` | v1: yes; v2: unless `contributes` is set | Entry script relative to the mod directory: `.tsx`, `.ts`, `.jsx`, `.js` or `.mjs` |
| `gameVersion` | no | Constraint on the host's `ManagerOptions.GameVersion`, such as `'>=0.2.0'` |
| `capabilities` | no | Capability names the mod requests |
| `dependencies` | no | Map of required mod IDs to version constraints, such as `com.example.core: ^1.0.0` |
| `optionalDependencies` | no (v2) | Map of optional mod IDs to version constraints; loaded first and checked when present, ignored when absent |
| `conflicts` | no (v2) | Map of incompatible mod IDs to version constraints; a matching pair is refused in either direction (`ErrConflictingMods`) |
| `platforms` | no (v2) | Supported target platforms (`macos`, `windows`, `linux`, `ios`, `tvos`, `watchos`, `android`, `harmonyos`, `web`, `miniprogram`); omitted means all platforms |
| `contributes` | no (v2) | Relative slash-separated content directories inside the mod: `assets`, `scenes`, `l10n`, and `datatables` |

Constraints accept `*`, `=`, `>=`, `>`, `<=`, `<`, `^` and `~` (or a bare version, equivalent to `=`), joined by spaces or commas. Clauses accept an optional leading `v` and partial versions (`MAJOR` or `MAJOR.MINOR`): `^1.2` means `>=1.2.0 <2.0.0`, `^0.2` and `^0.0` bound the next minor version, `~1.2` and `1.2` mean `>=1.2.0 <1.3.0`, `<=1.2` means `<1.3.0`, and `>1.2` means `>=1.3.0`. Comparisons use pure SemVer 2.0.0 precedence: build metadata is ignored, pre-releases (including Go pseudo-versions) match any range whose bounds bracket them (`>=1.0.0` matches `1.1.0-beta`, while `1.0.0-beta` does not), and `<2.0.0` (including the upper bound of `^1.0.0`) matches `2.0.0-alpha` unless written `<2.0.0-0`. Quote a value that starts with `>` or `*` (`'>=0.2.0'`, `'*'`), since YAML reads those characters at the start of a value as a folded block and an alias.

The format is strict: `ParseManifest` refuses unknown or misspelled fields, duplicate keys, a second YAML document, unknown or repeated capability or platform names, version 2 fields in a `manifestVersion: 1` file, and a `manifestVersion` that is missing, unsupported or not written as the plain integer `1` or `2`, all as `ErrInvalidManifest`. `EncodeManifest` writes back the `ManifestVersion` it read (so version 1 files round-trip byte for byte unchanged) in one canonical form: fields in the order above, capabilities in the order listed below, maps (`dependencies`, `optionalDependencies`, `conflicts`) sorted by ID in byte order, and `platforms` as a flow sequence sorted in byte order. A `ModManifest` built in Go may leave `ManifestVersion` at 0: `Validate` and `ResolveOrder` accept it, and `EncodeManifest` writes `manifestVersion: 2`. `Manager` reads only `mod.yaml`; a mod directory that still has only the old `mod.json` fails with a message asking for `mod.yaml`.

`ResolveOrder` orders both required `dependencies` and present `optionalDependencies` before their dependents (ignoring absent optional dependencies), refuses conflicting pairs in either direction with `ErrConflictingMods`, and keeps lexicographical ID tie-breaking.

Capabilities are bitflags on `Capability`: `CapReadState` (`readState`), `CapWriteState` (`writeState`), `CapSpawnEntity` (`spawnEntity`), `CapEmitEvent` (`emitEvent`), `CapUI` (`ui`), `CapAudio` (`audio`), and `CapStorage` (`storage`). Calls outside a mod's granted capabilities fail with `*PermissionError` without crashing the host.

`capabilities` in `mod.yaml` also accepts dotted host-defined capability names (`<namespace>.<name>`, dot-separated lower camel case segments such as `inventory.read` or `editor.contextAction`). Built-in names decode into `ModManifest.Capabilities`, while dotted names decode into `ModManifest.HostCapabilities` and are encoded after the built-in names in sorted byte order so round trips stay deterministic. A mod that requests a host capability not declared in `ManagerOptions.Capabilities` fails alone at load with `ErrUnknownCapability`, while sibling mods still load.

A mod without `CapStorage` runs with `tsx.Sandbox.DisableStorage` set, so every method on the `"nsgo"` module's `Storage` (`getItem`, `setItem`, `removeItem`, `clear`, `keys`) rejects with `tsx: Storage is disabled for this sandbox`, in addition to the `"game"` module's `storageGet`, `storageSet`, and `storageRemove` failing with `*PermissionError`. Disabling storage leaves any data already in the mod's partition in place, so granting `CapStorage` again exposes it once more.

## The `"game"` TSX module

Mods import `"game"` (`GameModule`) to hook fixed simulation steps, emit or listen for events, spawn prefabs, read/write scoped state, play audio, persist per-mod storage, and mount TSX UI panels. Export TypeScript definitions for mod authors with `modding.WriteGameAPIDTS("game.d.ts")`.

```tsx
import { Text, View } from "nsgo";
import { onStep, onEvent, spawn, emit, getState, mountPanel, log } from "game";

let elapsed = 0;
let wave = 0;

onStep((dt) => {
  elapsed += dt;
  if (elapsed >= 5) {
    elapsed = 0;
    wave++;
    spawn("slime", 120, 80, { hp: 30 });
    emit("boss-rush:wave", { wave });
  }
});

onEvent("boss_defeated", (payload) => {
  log("Defeated:", payload?.name, "score:", getState("score"));
});

mountPanel("boss-timer", () => (
  <View>
    <Text>Boss Rush Active</Text>
  </View>
), "hud");
```

## Host-registered TSX modules and custom capabilities

Games and runtime plugins can expose their own typed TSX modules alongside `"game"`, each gated by host-defined capabilities declared on `ManagerOptions.Capabilities` (`HostCapability`) and `ManagerOptions.Modules` (`ModModule`). The sample mod in [`pkg/modding/testdata/mods/loot-tracker/`](../../pkg/modding/testdata/mods/loot-tracker/) demonstrates a mod that requests `inventory.read` and imports a host `"mygame/inventory"` module.

```go
mgr := modding.NewManager(cx, modding.ManagerOptions{
	Capabilities: []modding.HostCapability{
		{Name: "inventory.read", Doc: "Read items in the player's inventory"},
		{Name: "inventory.write", Doc: "Add or remove items in the player's inventory"},
	},
	Modules: []modding.ModModule{
		{
			Name:       "mygame/inventory",
			Capability: "inventory.read",
			Doc:        "Read-only inventory queries.",
			Funcs: map[string]any{
				"countItem": func(cx *core.Scope, item string) (int, error) {
					api, err := modding.Caller(cx)
					if err != nil {
						return 0, err
					}
					if err := api.RequireHost("inventory.read", "inventory.countItem"); err != nil {
						return 0, err
					}
					return inventory.Count(item), nil
				},
			},
		},
	},
})
```

- `NewManager` validates `Capabilities` and `Modules`, refusing duplicate module names, collisions with `"game"` or NSGo's reserved `"nsgo"` prefix, invalid capability names, or modules without a declared capability. If a TSX runtime may already have started before `NewManager` runs (for example, in multi-session apps or tests), call `modding.RegisterModule` in `init()` so NSGo's TSX registry receives the module signatures before startup; each `NewManager` then binds its own live closures per manager without cross-manager state leakage.
- A mod's sandbox `Modules` allowlist contains `"game"` plus every host module whose `Capability` (or any entry of `Capabilities`, for multi-permission modules) the mod both requests in `mod.yaml` and is granted. An ungranted import therefore fails immediately at load rather than at first call.
- Host functions receive `*core.Scope` as their first parameter (or right after `context.Context` for async functions) and call `modding.Caller(cx)` to reach the calling mod's `*ModAPI` and `api.RequireHost(capability, action)` to enforce fine-grained checks that return `*PermissionError` (`ErrPermission` and `core.CodeAccessDenied`).
- Narrow host permissions globally with `ManagerOptions.GrantHost` (leave `nil` for no narrowing, or pass `[]string{}` to deny all host capabilities) and per mod at runtime with `Manager.SetHostGrant(id, caps)`. Narrowing a loaded mod with `SetHostGrant` takes effect immediately on the next call to any host module function; inspect declared capabilities via `Manager.HostCapabilities()` and effective per-mod grants via `ModInfo.HostCapabilities`.
- Write TypeScript declarations for `"game"` and every host module into a directory with `modding.WriteModAPIDTS(dir, opts)`, which writes `game.d.ts` plus one `<module>.d.ts` per host module from `tsx.Declarations()`. `WriteGameAPIDTS` remains available when only `"game"` is needed.

## Managing mod lifecycle in Go (`Manager`)

Create a `Manager` on your NSGo scope with a `GameBridge` connecting mod actions to your game engine, discover mods from disk or `io/fs.FS`, and step them from `game.Loop`:

```go
mgr := modding.NewManager(cx, modding.ManagerOptions{
	StorageKey:  "mods",
	GameVersion: "0.2.0",
	Bridge: modding.GameBridge{
		OnSpawn: func(req modding.SpawnRequest) (string, error) {
			return world.SpawnPrefab(req.Prefab, req.X, req.Y, req.Props)
		},
		OnPlaySound: func(req modding.SoundRequest) error {
			return audioMgr.Play(req.Name, req.Volume, req.Rate, req.Loop)
		},
	},
})
defer mgr.Close()

if _, err := mgr.Discover("mods"); err != nil {
	return err
}
_ = mgr.LoadAll() // validates manifests, resolves topological dependency order, isolates faults

// In game.Loop.Update:
mgr.Step(dt)

// Render mod-mounted HUD panels in your NSGo view tree:
hudPanels := mgr.PanelsView("hud")
_ = hudPanels
```

Control individual mods at runtime with `Enable`, `Disable`, `Install`, `InstallFS`, `Uninstall`, `Load`, `Unload`, `Reload`, `SetGrant`, `Emit`, `Call`, `List`, `Info`, and `Logs`. A panicking or throwing mod transitions to `StateFailed` while the host game and sibling mods keep running.

`Discover` and `DiscoverFS` isolate failures per mod instead of aborting the whole scan: a bad manifest, missing entry or `contributes` directory, incompatible `gameVersion`, unsupported platform (`ErrUnsupportedPlatform`, checked against `ManagerOptions.Platform` or the current runtime platform), duplicate ID (failing both mods with `ErrDuplicateMod`), conflict (failing both mods with `ErrConflictingMods`), unsatisfied dependency, dependent of a failed mod (`ErrModFailed`), or cycle records `StateFailed` with `Error` and `Dir` on `ModInfo` (visible via `List` and `Info`) while healthy mods are registered in load order and returned alongside the joined error.

### Content overlay (`ContentFS`)

Version 2 mods can contribute read-only content directories under `contributes` (`assets`, `scenes`, `l10n`, `datatables`), with or without an `entry` script. `Manager.ContentFS()` returns a concurrency-safe read-only `io/fs.FS` overlaying every currently loaded mod's `contributes` directories in load order so later mods win per path:

```go
contentFS := mgr.ContentFS()

// Pass assets subtree to pkg/assets:
assetsFS, _ := fs.Sub(contentFS, "assets")
assetMgr := assets.NewManager(cx, assets.WithFS(assetsFS))
_ = assetMgr

// Read overlaid localization tables, scenes, or data tables:
enYAML, _ := fs.ReadFile(contentFS, "l10n/en.yaml")
_ = enYAML
```

Paths in `ContentFS` are strictly confined to each mod's directory: path traversal and symlinks pointing outside a mod's directory (relative, absolute, or cyclic) are refused with `ErrUnsafePath` both during discovery and on read, matching Steam Workshop's path confinement rules. Unloading, disabling, or reloading a mod updates `ContentFS` immediately.

`Reload` validates the updated `mod.yaml` (syntax, ID, game version, platform, files, and dependencies/conflicts) before it unloads anything, so a broken manifest returns an error and the version already loaded keeps running.

`SetGrant` on a loaded mod takes effect at once: the `"game"` module checks the new capabilities from the next call, revoking `CapUI` removes the panels the mod mounted, and when the grant changes `CapAudio` or `CapStorage`, which the sandbox itself enforces, the mod is unloaded and loaded again in a sandbox built for them, which runs its entry script again. `Close` unloads every mod, after which `Info` reports each as `StateDiscovered` (or `StateDisabled`), and an unsubscribe function a mod kept from `onEvent` or `onStep` does nothing once the mod is unloaded.

Browsers cannot isolate a mod from the page: a mod running on the web can use the page's own APIs (`XMLHttpRequest`, `WebSocket`, `localStorage`) whatever its capabilities say. Loading a mod on the web therefore fails with `ErrWebUnisolated` unless you set `ManagerOptions.WebUnisolated`, which you should only do for mods you trust as much as your own code. Other targets isolate mods and ignore the option.

## Steam Workshop (`Workshop`)

`Workshop` connects Steamworks `ISteamUGC` to `Manager` so authors can publish mods and custom levels and subscribed players automatically download, validate, install, update, and remove them.

### Workshop metadata (`workshop.yaml`)

Publishing writes a deterministic `workshop.yaml` (`WorkshopMetadata`) beside `mod.yaml` so subsequent publishes update the same Steam Workshop item instead of creating duplicates:

```yaml
version: 1
appId: 480
publishedFileId: 123456789
title: Boss Rush
description: Spawns a slime every five seconds and shows a HUD badge while the rush runs.
tags: [bosses, gameplay]
visibility: public
preview: preview.png
```

| Field | Required | Meaning |
| --- | --- | --- |
| `version` | yes | Schema version of `workshop.yaml`, currently `1` (`modding.WorkshopMetadataVersion`) |
| `appId` | yes | Steam application ID that owns the Workshop item |
| `publishedFileId` | no | Assigned Steam Workshop `PublishedFileID` (populated automatically on first publish) |
| `title`, `description` | no | Override `mod.yaml`'s `name` and `description` on Workshop |
| `tags` | no | Steam Workshop tags (encoded sorted in byte order; empty or duplicate tags are refused) |
| `visibility` | no | One of `public`, `friendsOnly`, `private`, or `unlisted` (defaults to `public`) |
| `preview` | no | Relative slash-separated path inside the mod directory to a preview image |

Like `mod.yaml`, `ParseWorkshopMetadata` refuses unknown fields, duplicate keys, trailing YAML documents, invalid visibility/tag/preview values, and a missing or non-literal `version: 1` as `ErrInvalidWorkshopMetadata`. `EncodeWorkshopMetadata` emits canonical 2-space YAML with no timestamps or volatile fields so round trips are byte-for-byte stable.

### Publishing and syncing subscribed items

Use `NewSteamWorkshop` on macOS, Linux, and Windows to bind `libsteam_api.dylib`, `libsteam_api.so`, or `steam_api64.dll` via `purego` (no CGo). When the Steamworks library or running Steam client is absent—or on web, iOS, and Android—`NewSteamWorkshop` returns `ErrWorkshopUnavailable`. For tests and offline tools, use `NewMemoryWorkshop`, which simulates multi-user publishing, querying, subscribing, downloading, and updates on disk.

```go
backend, err := modding.NewSteamWorkshop(modding.SteamWorkshopOptions{})
if err != nil {
	// Fall back cleanly when Steam is not running (errors.Is(err, modding.ErrWorkshopUnavailable)).
	return err
}
defer backend.Close()

ws := modding.NewWorkshop(backend, mgr, modding.WorkshopOptions{
	AppID:    480,
	AutoLoad: true,
})

// Author: validate mod.yaml and publish or update the Workshop item.
res, err := ws.Publish(ctx, "mods/boss-rush", modding.PublishOptions{
	Tags:       []string{"gameplay", "bosses"},
	Visibility: modding.VisibilityPublic,
	Preview:    "preview.png",
	ChangeNote: "Initial release",
})
if errors.Is(err, modding.ErrWorkshopLegalAgreementRequired) {
	// Item ID is already saved in workshop.yaml; prompt author to open res.LegalAgreementURL.
	_ = res.LegalAgreementURL
}

// Player: query, subscribe, and reconcile Manager with all subscribed Workshop items.
if _, err := ws.SyncSubscribed(ctx); err != nil {
	return err
}
```

- `Publish` validates `mod.yaml`, entry script, preview path, and `workshop.yaml` before creating or updating any Steam Workshop item. When Steamworks reports that the author must accept the Steam Workshop Subscriber Agreement, `Publish` still saves `publishedFileId` to `workshop.yaml` and returns `PublishResult{NeedsLegalAgreement: true, LegalAgreementURL: WorkshopLegalAgreementURL}` alongside `ErrWorkshopLegalAgreementRequired`.
- `SyncSubscribed` unloads and uninstalls items that were unsubscribed, downloads missing or outdated items, checks path confinement (`WorkshopOptions.AllowedRoots` and escaping symlinks via `ErrWorkshopUnsafePath`), and installs valid mods in topological dependency order while isolating failures in broken sibling items. `InstallSubscribed` installs a single subscribed item on demand.
- For bundling the Steamworks shared library with `nsgo-natives.yaml`, passing `SteamWorkshopOptions.LibraryPath` on macOS, and configuring Workshop in the Steamworks Partner Portal, see [Shipping on Steam](steam.md#steam-workshop).
## Shared Steamworks lifecycle

`SteamWorkshop` should use the shared `steamworks.Client` when a game also has presence or overlay features. Closing Workshop must not shut down Steamworks while another feature remains active.
