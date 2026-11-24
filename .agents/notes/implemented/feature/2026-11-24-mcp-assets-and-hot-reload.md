# Agent Note: MCP assets inspection and hot reload

Status: implemented

## Problem

Agents debugging or iterating on a running NSGo game over `nsgo mcp` (see [MCP scene, layer, entity and component inspection](2026-11-24-mcp-scene-entity-inspection.md)) could inspect scenes, layers, and entities, but had no way to see which asset groups were loaded, how much memory they held, or to reload edited assets such as sprite atlases, shaders, materials, and YAML resources on demand without restarting the game.

## Decision

Extend `packages/game/mcp` with a dedicated `assets.go` tool group (`game_assets_list`, `game_asset_reload`, `game_assets_reload_group`) and minimal additive `Config` hookup in `mcp.go`, backed by read-only stats and synchronous in-place reload helpers in `packages/game/assets`, `packages/game/tooling/hotreload`, and `packages/game/resource`.

## Architecture and tool contract

- **Minimal `mcp.go` surface for parallel work**: All asset tool constants, schemas, result structs, handlers, and `RegisterAsset` / `RegisterAtlas` helpers live in `packages/game/mcp/assets.go`. `mcp.go` only adds optional `Config` fields (`Assets`, `AssetsFunc`, `Resources`, `Shaders`, `Materials`, `ReloadAsset`), one unexported `customAssets` registry pointer on `Bridge`, and `b.assetTools()` in `Bridge.Tools()`.
- **Deterministic group listing and memory accounting**: `packages/game/assets/stats.go` adds side-effect-free `Manager.GroupNames`, `Manager.LookupGroup`, `Manager.GroupStats`, and `Group.Stats` using untracked signal reads so MCP calls never subscribe to reactive signals or lazily create phantom groups. `Group` tracks raw file byte sizes alongside loaded resources (`rawBytes`) and combines them with pixel buffers (`ImageSource.Pixels`), inline byte payloads (`ImageSource.Data`), declared image/atlas dimensions, and decoded PCM duration estimates for sounds. `game_assets_list` merges `assets.Manager` groups, loaded `resource.Library` paths, `ShaderReloader` shaders, `MaterialReloader` materials, and custom registered assets, sorted deterministically by `(name, source)` and per-group `key`.
- **Off-goroutine fetch, engine-goroutine apply**: `automation.Call` executes tool handlers synchronously on the engine goroutine, and a handler cannot wait for work posted back to it. `Manager.ReloadAsset` and `Manager.ReloadGroup` therefore read updated bytes through each group's configured reader (`g.readAssetFunc()`), parse atlases and run custom loaders on a separate goroutine, as `Group.Load` does, wait for it (or for the call's context, after which unapplied custom values are released), and then update loaded assets in place on the engine goroutine (`*cur = *next` for `*sprite.Atlas`) before incrementing `Manager.Revision()` once. Existing `*sprite.Atlas` pointers held by running game code see updated frames immediately and reactive canvases redraw on `Revision()`. Sounds, images without a file and files at a density the screen does not show are reported as skipped: a sound decodes asynchronously on the platform, so its result could not be reported in the same call, and replacing an unshown density would drop the source's other scales, as `Manager.Reload` already avoids.
- **Structured reload diagnostics vs protocol errors**: Argument validation errors (unknown JSON fields via `DisallowUnknownFields()`, missing `key`/`group`, invalid `state`, conflicting `key` + `all`), unconfigured/disposed managers, and unknown asset/group lookups return Go errors to `automation.Call`. Parse, read, schema, or shader compilation errors on a matched asset preserve the previously working asset and return a structured JSON `AssetReloadResult` / `AssetsReloadGroupResult` with `reloaded: false` and the diagnostic message in `error`, keeping the live game running.
- **Unified reload across hot-reload subsystems**: Small additive `ReloadFile` / `ReloadShaderFile` / `ReloadMaterialFile` and path getters in `packages/game/resource` and `packages/game/tooling/hotreload` (building on [Shader and material hot reload](2026-11-24-shader-material-hot-reload.md)) allow `game_asset_reload` and `game_assets_reload_group` to reload atlases, images, bytes, custom assets, shaders, materials, and YAML resources uniformly from their configured file systems.

## Alternatives considered

### Re-using `Group.Release()` followed by `Group.Load()` for MCP reloads

Releasing and asynchronously reloading a group would require no new per-spec reload code in `packages/game/assets`. It was rejected for two reasons: (1) `Group.Release()` + `Group.Load()` allocates new `*sprite.Atlas` pointers, so game systems holding an existing `*sprite.Atlas` pointer would never see the update; and (2) `Group.Load()` completes asynchronously via `core.Scope.Post`, which cannot be waited on synchronously inside an `automation.Call` handler on the engine goroutine without deadlocking.

### Returning reload parse/compile failures as raw tool call errors

Returning parse or shader compile errors as top-level Go errors from `game_asset_reload` would match argument errors. It was rejected because reloading an asset (or a whole group via `game_assets_reload_group` / `all: true`) can partially succeed and benefits from returning timing (`duration_ms`), preserved `bytes`, `group`, per-item status (`items`), and structured `error` details alongside `reloaded: false` so agents can diagnose broken edits cleanly.

### Routing all reloads exclusively through `hotreload.Watcher`

Delegating MCP reloads solely to `hotreload.Watcher` would avoid adding `ReloadAsset` / `ReloadShaderFile` / `ReloadMaterialFile`. It was rejected because `hotreload.Watcher` is a polling loop compiled out under `-tags release` and games using `assets.WithFS` or `nsgotest` often run without an active filesystem polling watcher; direct on-demand reload works deterministically in both `nsgo dev -automation` and `nsgo test`.

## Consequences

- **Benefits**: Coding agents can inspect loaded asset groups, states, asset keys, and memory usage and trigger deterministic in-place reloads of atlases, images, bytes, custom assets, shaders, materials, and YAML resources over `nsgo mcp`, immediately updating the running game while keeping broken edits non-fatal.
- **Costs**: `packages/game/assets` maintains `rawBytes` per loaded asset for memory accounting and an on-demand reload path alongside `Manager.Reload(uri, data)`. Memory figures for URI-only images and decoded sounds without inline pixel/byte buffers are estimates derived from declared dimensions, frame extents, or audio duration.

## Verification

Verified with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./mcp`, `go tool nsgo test ./assets`, `go tool nsgo test ./resource`, `go tool nsgo test ./tooling/hotreload`, `go tool nsgo test -run TestStabilityTable .`, `go run ./tools/changelog check`, and `bun run verify-notes`.
