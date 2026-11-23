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

Control individual mods at runtime with `Enable`, `Disable`, `Load`, `Unload`, `Reload`, `SetGrant`, `Emit`, `Call`, `List`, `Info`, and `Logs`. A panicking or throwing mod transitions to `StateFailed` while the host game and sibling mods keep running.
