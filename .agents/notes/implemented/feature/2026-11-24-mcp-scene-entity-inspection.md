# Agent Note: MCP scene, layer, entity and component inspection

Status: implemented

## Problem

`nsgo mcp` inspects native NSGo view trees only, whereas a game built on `packages/game` renders into a single `Canvas` or GPU view. Coding agents running a game under `nsgo dev -automation` have no way to inspect the active scene stack, layer draw order, live ECS entities, or component values across simulation steps.

## Decision

Add an experimental `packages/game/mcp` package built on `github.com/nsgo-dev/nsgo/automation` (`automation.Register`) that registers game inspection tools on a child `core.Scope` of the app's graph, accompanied by minimal read-only inspection helpers in `packages/game/scene` and `packages/game/entities`.

## Architecture and tool contract

- **Bridge and tool-group layout**: `mcp.New(cfg)` and `mcp.Attach(cx, cfg)` create a `*mcp.Bridge` whose `Attach(cx)` creates a child scope via `cx.Child()`, registers every tool from `b.Tools()` onto that child scope, and disposes the child scope immediately on any registration error so partial tool sets never leak. Disposing `cx` or calling `b.Detach()` unregisters all tools atomically. Tool groups live in dedicated files (`inspect.go` for #461) and append to `b.Tools()`, giving follow-up MCP issues (#462–#469) a single composition point without changing registration lifecycle code.
- **Tool naming**: Tools use `snake_case` with a `game_` prefix (`game_scenes_list`, `game_layers_list`, `game_entities_list`, `game_entity_inspect`, `game_component_get`). This matches NSGo's own `automation` convention (`current_scene`, `set_count`, and single-word built-ins), passes `nsgo`'s `ValidateAppTool`, avoids collisions with built-in tools, and remains accepted by MCP/LLM clients that disallow dots in tool names.
- **Strict argument decoding**: Every handler decodes arguments with `json.Decoder.DisallowUnknownFields()` so mistyped parameter names fail loudly rather than being ignored silently.
- **Stable generational entity references**: Entities are addressed by `"entity:<index>:<generation>"` (`FormatEntityRef` / `ParseEntityRef`), directly backed by `entities.Entity`'s 32-bit slot index and 32-bit generation. Surviving entities keep identical refs across simulation steps regardless of other spawns or destructions; destroyed entities whose slots are recycled receive a higher generation so stale refs fail with `"not alive"` rather than aliasing new entities.
- **Zero-reflection `entities`, safe reflection in `mcp`**: `packages/game/entities` preserves its zero-reflection constraint (`entities/doc.go`) and adds read-only helpers (`MakeEntity`, `Resolve`, `CodecNames`, `CodecName`, `HasCodec`, `EncodeComponent`, `EntityComponents`, and side-effect-free `ParentOf`/`ChildrenOf` that never lazily instantiate `Hierarchy2D`). `packages/game/mcp` reads codec-registered components automatically, surfaces built-in `Transform2D`, `WorldTransform2D`, and `sceneformat.ObjectRef` unless shadowed by a codec or bridge registration, and supports transient runtime components via `RegisterComponent[T]` and `RegisterComponentFunc`. Component encoding uses `encoding/json` on the fast path and falls back to a bounded reflection sanitizer on failure so non-finite floats (`NaN`, `+Inf`, `-Inf`), callbacks, channels, and pointer cycles remain inspectable when debugging broken state.
- **Additive `scene` helpers**: `scene.Manager.Stack()` exposes the active controller stack from bottom to top, and `scene.Layer.Parallax()` exposes effective parallax factors (including the `1.0` default for zero struct literals). Both are additive methods compatible with `game/scene`'s Stable status.

## Alternatives considered

### Dot-separated tool names (`game.scenes.list`)

Dot-separated names visually mirror Go package hierarchies and pass `nsgo`'s `ValidateAppTool` regex (`^[a-zA-Z0-9_.-]{1,128}$`). They were rejected because major LLM tool-calling APIs (such as OpenAI and Anthropic) restrict tool names to `[a-zA-Z0-9_-]`, which would force clients to rewrite names or reject the tool catalog, and NSGo's own automation examples use `snake_case`.

### Reflecting directly over `entities.World` internal stores

Inspecting every component store inside `packages/game/entities` via `reflect` would expose unregistered components without calling `RegisterComponent`. This was rejected because `entities` explicitly commits to zero reflection and keeping transient handles (audio voices, GPU buffers, callbacks) out of generic serialization unless opted in; combining automatic `RegisterCodec` discovery, built-in transform/ref defaults, and `mcp.RegisterComponent` preserves the boundary while covering both saved and transient state.

### Calling `World.Snapshot()` and `World.Hierarchy2D()` directly from `mcp`

Building `mcp` solely on existing exported `entities` methods would avoid touching `packages/game/entities`. It was rejected because `World.Snapshot()` marshals every entity and codec in the world on every single-entity lookup, and `World.Hierarchy2D()` lazily creates and attaches a `Hierarchy2D` and transform stores as a side effect of a read-only inspection call.

## Consequences

- **Benefits**: Agents running `nsgo mcp` can discover scenes, draw-ordered layers, live entities, hierarchy links, and component values with stable refs across steps, including non-finite or cyclic runtime values during debugging. Future MCP tool groups (#462–#469) plug cleanly into `Bridge.Tools()`. Release builds incur zero overhead because `automation.Register` compiles to a no-op outside automation/test builds.
- **Costs**: Games that want transient (non-codec) custom components inspected must register them with `mcp.RegisterComponent` or `mcp.RegisterComponentFunc`. `game/entities` and `game/scene` gain small read-only helper methods that must be maintained alongside existing snapshot and stack internals.

## Verification

Verified with `go tool nsgo test ./mcp/...`, `./scene/...`, `./entities/...`, and `.` (stability table), `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, `gofmt`, and `bun run verify-notes`.
