# modding

`game/modding` loads and runs capability-sandboxed TSX/JS game mods on top of NSGo's `tsx` runtime, with `mod.json` manifest validation, topological dependency resolution, fault isolation, persisted enable/disable state, and a `"game"` module API.

```go
import "github.com/nsgo-dev/game/packages/game/modding"
```

## Mod manifest (`mod.json`) and capabilities

Each mod lives in its own directory with a `mod.json` (`ModManifest`) and an entry script (`mod.tsx` or `mod.js`):

```json
{
  "id": "com.example.boss-rush",
  "name": "Boss Rush",
  "version": "1.2.0",
  "entry": "mod.tsx",
  "gameVersion": ">=0.2.0",
  "capabilities": ["readState", "spawnEntity", "emitEvent", "ui", "storage"],
  "dependencies": {
    "com.example.core-lib": "^1.0.0"
  }
}
```

Capabilities are bitflags on `Capability`: `CapReadState` (`"readState"`), `CapWriteState` (`"writeState"`), `CapSpawnEntity` (`"spawnEntity"`), `CapEmitEvent` (`"emitEvent"`), `CapUI` (`"ui"`), `CapAudio` (`"audio"`), and `CapStorage` (`"storage"`). Calls outside a mod's granted capabilities fail with `*PermissionError` without crashing the host.

## The `"game"` TSX module

Mods import `"game"` (`GameModule`) to hook fixed simulation steps, emit or listen for events, spawn prefabs, read/write scoped state, play audio, persist per-mod storage, and mount TSX UI panels. Export TypeScript definitions for mod authors with `modding.WriteGameAPIDTS("game.d.ts")`.

```tsx
import { onStep, onEvent, spawn, getState, mountPanel, log } from "game";

let elapsed = 0;

onStep((dt) => {
  elapsed += dt;
  if (elapsed >= 5) {
    elapsed = 0;
    spawn("slime", 120, 80, { hp: 30 });
  }
});

onEvent("boss_defeated", (payload) => {
  log("Defeated:", payload?.name, "score:", getState("score"));
});

mountPanel("boss-timer", () => <text>Boss Rush Active</text>, "hud");
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
