# Agent Note: Default assets.Manager "asset:" reads to core.Scope.ReadAsset

Status: implemented

## Problem

`assets.Manager` previously resolved `"asset:"` URIs through two different roots depending on asset kind: images (`core.Scope.PrefetchImage`) and sounds (`audio.LoadSound`) resolved NSGo's packaged `resources/` directory (fetched on demand on the web), whereas byte-backed loads (`LoadBytes`, `LoadAtlas` JSON, `LoadCustom`, and their `Group` counterparts) fell back to `os.DirFS("assets")` relative to the working directory when `WithFS` and `WithReadAsset` were unset. On `js/wasm` (no local filesystem) and packaged macOS, iOS, or Android bundles (where cwd is not the project directory), byte reads failed unless every game embedded its entire asset tree with `//go:embed`, inflating WebAssembly bundles and duplicating data on disk.

## Decision

When `Manager.ReadAsset` is nil and `WithFS` is not set, `assets.Manager` reads `"asset:"` URIs through `core.Scope.ReadAsset` so every `"asset:"` URI resolves the same `resources/` store on every target:

- `Manager.readScopeAsset` posts onto `m.cx`, spawns a short-lived child scope via `m.cx.Child()`, and invokes `sub.ReadAsset(uri, done)`. Disposing `sub` in `done` prevents child scopes from accumulating on `m.cx`; cancelling `ctx` (such as on `Group.Release` or sibling load failure) disposes `sub` via `m.cx.Post`, propagating cancellation to the platform reader (aborting in-flight browser `fetch` requests).
- `WithFS` and `WithReadAsset` remain full overrides (including `assets.WithFS(os.DirFS(hotreload.DevAssetsDir()))` under `game dev`), and `Group.SetFS` still checks its mounted pack first before falling back to the manager reader.
- `ReloadAsset` and `ReloadGroup` wait synchronously on the engine goroutine while `fetch` runs off-thread (until `nsgo-dev/nsgo#2342` supports async automation tool handlers). Because `core.Scope.ReadAsset` delivers its callback through `owner.Post` on the engine goroutine, calling `readScopeAsset` inside `ReloadAsset`/`ReloadGroup` would deadlock; `Group.reloadReadAssetFunc` therefore refuses `"asset:"` reloads when neither `WithFS` nor `WithReadAsset` is configured and returns an explicit error instead of hanging.
- `assetbuild.Builder.ResourcesDir` and the `-resources` flag on `game assets` and `game manifest` redirect built asset outputs and manifest scans into `resources/` with `embed` disabled, and `examples/caverun` streams its level group from `resources/` while keeping only its title bitmap font embedded.

## Alternatives considered

- **Keep `os.DirFS("assets")` as default and require apps to wire `cx.ReadAsset` through `WithReadAsset`**: rejected because it leaves the split between image/sound `"asset:"` URIs and byte `"asset:"` URIs broken by default on web and mobile builds and forces boilerplate into every game.
- **Let `ReloadAsset` call `readScopeAsset` directly**: rejected because `runReloads` blocks the engine goroutine while waiting for `fetch`, guaranteeing a deadlock whenever `Scope.ReadAsset` tries to post its completion back to the engine goroutine.
- **Rewrite `game.yaml` schema with a new top-level `resources` boolean**: rejected because `game.yaml` targets already accept `out: resources/...` and `manifest: {in: resources, embed: false}` natively; adding `Builder.ResourcesDir` and CLI `-resources` avoids a schema change while supporting projects whose `game.yaml` targets `assets/`.

## Consequences

- **Benefits**: One consistent rule for `"asset:"` URIs across images, sounds, atlases, maps, and custom data on every target; web games such as Cave Run stream level assets on demand instead of embedding them in `app.wasm`; cancellation aborts platform reads cleanly; `ReloadAsset` without `WithFS` fails fast instead of deadlocking.
- **Costs and known limits**: Callers that relied on an unconfigured `assets.Manager` reading `./assets/` from the process working directory must either move runtime files to `resources/` or pass `assets.WithFS(os.DirFS("assets"))`. `ReloadAsset` and `ReloadGroup` still require `WithFS` (such as `os.DirFS(hotreload.DevAssetsDir())`) or `WithReadAsset` until automation handlers become asynchronous (`nsgo-dev/nsgo#2342`).
