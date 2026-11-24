# modding

`game/modding` loads and runs capability-sandboxed TSX/JS game mods on top of NSGo's `tsx` runtime, with `mod.yaml` manifest validation, topological dependency resolution, fault isolation, persisted enable/disable state, and a `"game"` module API.

```go
import "github.com/nsgo-dev/game/packages/game/modding"
```

## Mod manifest (`mod.yaml`) and capabilities

Each mod lives in its own directory with a `mod.yaml` (`ModManifest`) and an entry script (`mod.tsx` or `mod.js`). The sample mod in [`packages/game/modding/testdata/mods/boss-rush/`](../../packages/game/modding/testdata/mods/boss-rush/) is the manifest and script shown on this page, and the package's tests load it:

```yaml
manifestVersion: 1
id: com.example.boss-rush
name: Boss Rush
version: 1.2.0
author: Example Studio
description: Spawns a slime every five seconds and shows a HUD badge while the rush runs.
entry: mod.tsx
gameVersion: '>=0.2.0'
capabilities: [readState, spawnEntity, emitEvent, ui]
```

| Field | Required | Meaning |
| --- | --- | --- |
| `manifestVersion` | yes | Schema version of the file, currently `1` (`modding.ManifestVersion`) |
| `id` | yes | Unique ID of letters, digits, `.`, `-` and `_`, such as `com.example.boss-rush` |
| `name` | yes | Human-readable name |
| `version` | yes | The mod's own semantic version (`MAJOR.MINOR.PATCH[-prerelease]`) |
| `author`, `description` | no | Free text |
| `entry` | yes | Entry script relative to the mod directory: `.tsx`, `.ts`, `.jsx`, `.js` or `.mjs` |
| `gameVersion` | no | Constraint on the host's `ManagerOptions.GameVersion`, such as `'>=0.2.0'` |
| `capabilities` | no | Capability names the mod requests |
| `dependencies` | no | Map of mod IDs to version constraints, such as `com.example.core-lib: ^1.0.0` |

Constraints accept `*`, `=`, `>=`, `>`, `<=`, `<`, `^` and `~`, joined by spaces or commas. Quote a value that starts with `>` or `*` (`'>=0.2.0'`, `'*'`), since YAML reads those characters at the start of a value as a folded block and an alias.

The format is strict: `ParseManifest` refuses unknown or misspelled fields, duplicate keys, a second YAML document, unknown or repeated capability names, and a `manifestVersion` that is missing, unsupported or not written as the plain integer `1`, all as `ErrInvalidManifest`. `EncodeManifest` writes a manifest in one canonical form (fields in the order above, capabilities in the order listed below, dependencies sorted by ID in byte order), so a file already in that form is unchanged by a read and a write. A `ModManifest` built in Go may leave `ManifestVersion` at 0: `Validate` and `ResolveOrder` accept it, and `EncodeManifest` writes `manifestVersion: 1`. `Manager` reads only `mod.yaml`; a mod directory that still has only the old `mod.json` is refused with a message asking for `mod.yaml`.

Capabilities are bitflags on `Capability`: `CapReadState` (`readState`), `CapWriteState` (`writeState`), `CapSpawnEntity` (`spawnEntity`), `CapEmitEvent` (`emitEvent`), `CapUI` (`ui`), `CapAudio` (`audio`), and `CapStorage` (`storage`). Calls outside a mod's granted capabilities fail with `*PermissionError` without crashing the host.

NSGo gives every sandbox the `"nsgo"` module's `Storage`, in a partition of its own, with no capability to withhold it. A mod without `CapStorage` therefore gets a partition quota of one byte, the smallest NSGo accepts, so it can keep at most one entry whose key and value total one byte (such as `setItem("a", "")`) and every larger `Storage.setItem` call fails; reading still sees what the partition already holds, such as what the mod wrote while it had the capability.

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

`Reload` validates the updated `mod.yaml` (syntax, ID, game version and dependencies) before it unloads anything, so a broken manifest returns an error and the version already loaded keeps running.

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
