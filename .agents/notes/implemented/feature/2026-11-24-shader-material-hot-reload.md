# Agent Note: Shader and material hot reload in game dev

Status: implemented

## Problem

`game dev` and `game/tooling/hotreload` previously reloaded atlases, images, raw bytes (`WatchAssets`), and shared 2D/PBR resources (`WatchResources`), but did not reload WGSL/Kage GPU shaders or 3D shader material definitions (#366). Iterating on 3D shaders and render state required restarting the app, and naive in-place replacement would crash or black out the viewport whenever an edit introduced a WGSL/Kage compile error, missing entry point, invalid pipeline state, or malformed material YAML.

## Decision

`game/tooling/hotreload` provides candidate-before-swap hot reload for shaders, GPU render pipelines, and 3D shader materials:

- **Shader compilation and diagnostics (`CompileShaderSource`, `ShaderReloader`, `ReloadError`)**: `.wgsl` files compile with `gpu.CompileShader` and `.kage` files compile with `gpu.CompileKage`. Compiler and pipeline-creation failures are normalized into `ReloadError` values wrapping `*loaderr.Error` (extracting 1-based line and column when reported) and stored per registered shader/pipeline name until a subsequent edit succeeds.
- **Atomic candidate-before-swap pipelines**: When a tracked shader file changes, `ShaderReloader.ReloadShader` compiles the candidate `*gpu.ShaderModule` and builds a candidate `*gpu.Pipeline` for every dependent `PipelineSpec` before swapping under lock. On any compile, `Build`, or `Device.CreatePipeline` error, the previous working `*gpu.Pipeline` stays active and `OnError` fires without panicking. When a candidate succeeds, the active pointer swaps atomically, the replaced pipeline is closed, the error clears, and `Version` / `Revision` advance.
- **Text 3D shader material format and dependency tracking (`MaterialDef`, `MaterialReloader`)**: `MaterialDef` is a versioned YAML format (`version: 1`, refusing unknown fields via `yaml.Decoder.KnownFields(true)` and trailing documents, rejecting non-finite numbers, invalid CSS colors, and non-portable paths containing `\` or `..`, and round-tripping deterministically via `EncodeMaterial`). It declares `shader` (or split `vertexShader` / `fragmentShader`), entry points, GPU pipeline state (`blend`, `cull`, `frontFace`, `depthWrite`, `depthCompare`, applied with `ApplyToPipelineDesc`), and typed uniform parameters (`scalars`, `vec2`, `vec3`, `vec4`, `colors`, `textures`). `MaterialReloader` registers as a listener on `ShaderReloader`, so editing a referenced shader automatically rebuilds and swaps every dependent material's pipeline.
- **Compound extension matching in `Watcher.WatchPattern`**: `matchesExt` uses `strings.HasSuffix(strings.ToLower(name), normExt)` instead of `filepath.Ext`, allowing multi-dot suffixes like `.material.yaml` and `.mat.yaml` without matching unrelated `.yaml` files.
- **Release build behavior**: `Watcher.WatchShader`, `Watcher.WatchShaders`, and `Watcher.WatchMaterials` compile to no-ops under `-tags release` (matching `WatchAssets` and `WatchResources`), while `ShaderReloader`, `MaterialReloader`, and `MaterialDef` remain available in release builds so games share one loading path across dev and release.
- **Shutdown safety**: `RegisterShaderBytes`, `RegisterPipeline`, `ReloadShader`, `RegisterBytes`, `ReloadMaterial`, and `onShaderReloaded` check `closed` under lock before installing a newly created `*gpu.Pipeline` and close any discarded candidate so concurrent `Close()` never leaks GPU pipelines.

## Alternatives considered

### Why not extend `resource.Material` instead of introducing `MaterialDef`?

Reusing `resource.Material` (which already reloads through `Watcher.WatchResources`) would avoid a second YAML material type. It was rejected because `resource.Material` models shared 2D/PBR surface properties (`type: material`, 2D canvas composite operations, base color/roughness/metallic) with no dependency on `nsgo/gpu` and no shader paths, entry points, GPU blend/cull/depth-stencil state, or custom uniform parameters. Keeping `MaterialDef` in `tooling/hotreload` keeps `game/resource` free of GPU pipeline dependencies while letting `WatchResources` and `WatchMaterials` coexist cleanly (both ignore files they have not loaded or registered).

### Why not watch shaders inside `cmd/game dev` as a separate CLI watcher process?

Compiling shaders inside the `game dev` CLI process could log errors even before the app opens a GPU device. It was rejected because `gpu.Device.CreatePipeline` validation (entry points, vertex layouts, target formats, depth-stencil state) requires the running app's live `*gpu.Device`, and `game dev` already passes the watched asset directory via `NSGO_GAME_ASSETS` (`hotreload.DevAssetsDir()`), delivering edits on the engine goroutine within the default ~190ms poll+debounce window (well under the 1-second target).

### Why not rebuild all pipelines on every file change without tracking dependencies?

Rebuilding every pipeline on any `.wgsl`/`.kage` save is simpler than tracking `vertPath`/`fragPath` and material listeners. It was rejected because unrelated shader edits would stall frames and could fail or churn pipelines that did not change; explicit path tracking limits recompilation and error attribution to affected shaders, pipelines, and materials.

## Consequences

- **Benefits**:
  - Edited `.wgsl`, `.kage`, and material YAML files update rendered output in under a second under `game dev` with default watcher timings.
  - Broken shader or material edits never crash or drop the active pipeline; structured `ReloadError` / `*loaderr.Error` diagnostics report file, line, and column and clear automatically upon recovery.
  - Games write one setup path (`NewDevWatcher` + `WatchShaders` / `WatchMaterials`) that becomes zero-cost file watching in `-tags release` builds.
- **Costs & limits**:
  - Two material YAML schemas coexist in the repository: `resource.Material` (`type: material`, for shared 2D/PBR scene materials) and `hotreload.MaterialDef` (`version: 1`, for custom GPU shader pipelines). Docs explain when to use each.
  - `MaterialDef` is not yet registered in `cmd/gameschema`; when schema publication expands beyond `sceneformat`, `MaterialDef` should be added alongside the other YAML formats.
  - `Watcher` uses polling (`DefaultPollInterval = 150ms`, `DefaultDebounce = 40ms`) rather than OS filesystem events, trading a small background stat cost for cross-platform reliability and atomic-save stability.

## Verification

- `go tool nsgo test ./tooling/hotreload/... ./cmd/game/... .` covers sub-second WGSL pixel reload via `gpu.SoftwareRenderer` under default `NewDevWatcher` timings, WGSL compile and missing-entry-point error fallback and recovery, Kage compile error line extraction, `MaterialDef` deterministic YAML round-trip and validation refusals, material reload and shader-to-material dependency tracking (including split vertex/fragment shaders and shared shaders), post-`Close` safety, and nil `NewDevWatcher` behavior.
- `go tool nsgo check -target macos,windows,linux,android,web` vets all supported cross-compilation targets.
- `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, `git ls-files -z -- '*.go' | xargs -0 -r gofmt -l`, and `bun run verify-notes` enforce formatting, schema, changelog, and note gates.
