# Agent Note: Per-sprite effects in game/sprite

Status: implemented

## Problem

`Sprite.Tint` (#29) multiplies a sprite's source channels through NSGo's `DrawTintedImageFrom` (nsgo#1111), which cannot lighten dark pixels toward white for hit flashes, draw solid silhouettes of arbitrary color, add alpha outlines, dissolve sprites, or remap palettes. `game/effects` (#42) compiles Kage shaders over a full game view rather than individual sprites inside a `sprite.Batch`. Meanwhile, NSGo's `core.Drawing` has no per-draw `SaveLayer` or `DrawShader` op, and canvas composite ops like `CompositeSourceAtop` affect pixels already drawn beneath the sprite.

Games need per-sprite solid-color flash and silhouette, alpha outline, dissolve, palette swap, and custom pixel effects that:

- never affect neighboring sprites or the background drawn under the sprite;
- allow parameters to mutate every frame without unbounded memory growth;
- keep plain sprites (and sprites with inactive effects) on `Batch`'s `DrawAtlas` fast path; and
- avoid bloating the minimal WebAssembly probe (`cmd/wasmprobe`) with the Kage shader compiler or image decoders when unused.

## Decision

`sprite.Sprite` gains an optional `Effect *Effect` pointer supporting `Flash` (with `Silhouette` shorthand), `Outline`, `Dissolve`, `PaletteSwap`, and a `Custom` per-pixel hook, backed by two complementary paths:

1. **In-batch pixel-buffer path (`Batch.Draw`)**: when an atlas exposes readable pixels (`Atlas.Image.Pixels` or lazily decoded `Atlas.Image.Data`), `Batch.Draw` evaluates the sprite's frame rectangle alone (never sampling outside the frame bounds, preventing atlas neighbor bleed) into a reused per-slot `*image.RGBA` in `Batch.fxPool`, expands the drawn rectangle by `ceil(Outline.Thickness)` pixels on each side when `Outline` is active, folds `Sprite.Tint` into the buffer, and records `DrawImageIn` with a monotonic process-unique `ImageSource.Version` (`nextEffectVersion`). Unchanged static effects hit the slot's cache key and skip pixel recomputation and version bumps; changing parameters reuse the slot's pixel slice in place (reslicing when capacity suffices) and trim idle tail slots back down so memory stays `O(active effected sprites in frame)`.
2. **URI-only fallback (`Batch.Draw`)**: when an atlas only specifies a `URI` (`core.Src("asset:...")` with neither `Pixels` nor `Data`), full dissolve (`Threshold >= 1`) skips drawing, `Flash` falls back to `DrawTintedImageFrom` (lerping white toward `Flash.Color` by `Strength`, exact for black silhouettes and darkening flashes), and other stages draw the sprite untinted without changing canvas composite ops.
3. **GPU Kage shader path (`EffectProgram`, `Effect.Uniforms`, `EffectShader`)**: compiles a Kage fragment shader on first call inside `EffectProgram()` (guarded by a mutex rather than a package-level `sync.OnceValues` variable, so Go's linker strips the shader compiler from binaries that never call it). This lets standalone `core.Shader` hero/boss sprites animate uniforms via `core.AnimatedNode` on the platform's frame clock even with `asset:` URI atlases on GPU-backed targets.

### Stage ordering and invariants

Effects always run in fixed order: `PaletteSwap` -> `Sprite.Tint` -> `Dissolve` -> `Flash` -> `Outline` -> `Custom`.

- `PaletteSwap` must run before `Tint` and `Flash` so entries match original atlas colors.
- `Outline` must run after `Dissolve` and `Flash` so outlines hug dissolving silhouettes and keep their own border color during a hit flash.
- `Batch` must never retain `*Sprite` pointers in `fxPool`, and must never share one `*image.RGBA` across two effected sprites in the same `Draw` call (NSGo copies `ImageSource.Pixels` after `Canvas.Draw` returns).
- `Effect` does not blank-import `image/png` or `image/jpeg`; `resolveAtlasPixels` calls `image.Decode`, honoring whatever decoders the application imports without inflating `wasmprobe`.

## Alternatives considered

### Why not canvas `CompositeSourceAtop` / `CompositeLighter` passes directly on `core.Drawing`?

Drawing a solid rectangle with `CompositeSourceAtop` on the main canvas affects every opaque pixel already drawn under the sprite (backdrop and lower-Z neighbors), directly violating per-sprite isolation. Additive overdraw (`CompositeLighter`) cannot flash pure black pixels to white, nor can it implement alpha outlines, dissolve, or palette swaps.

### Why not `core.OffscreenImage` per sprite?

`core.OffscreenImage` requires a `*core.Scope` to allocate and release, is not supported on Android yet, and only records 2D canvas ops — it still cannot perform per-pixel dissolve, palette swap, or solid-color outlines on arbitrary shapes.

### Why not a package-level `sync.OnceValues` variable for `EffectProgram`?

A package-level `var compileEffectProgram = sync.OnceValues(...)` references `core.CompileShader` from package initialization, forcing Go's linker to retain the Kage compiler in every binary importing `sprite`. Measuring `./cmd/wasmprobe` showed this added 1.46 MiB raw / 248 KiB gzipped and breached `benchmarks/web-budget.json`. Moving the lazy check inside `EffectProgram()` with a `sync.Mutex` keeps `wasmprobe` within budget (`7.59 MiB` raw / `2.05 MiB` gzipped).

### Why not a single global or per-atlas LRU cache keyed by effect parameters?

A parameter-keyed cache grows unboundedly when `Flash.Strength` or `Dissolve.Threshold` changes every frame and would thrash on animated sprites. Reusing per-slot scratch buffers on `Batch` with exact single-entry cache validation gives zero steady-state allocations for both static and every-frame-animated sprites.

## Consequences

- **Benefits**: exact pixel-identical flash, silhouette, outline, dissolve, palette swap, and custom effects on every NSGo target (including HarmonyOS, mini programs, and headless `gametest.RenderOps`); zero impact on neighboring sprites or background; zero regression on plain `DrawAtlas` batches; zero pixel-buffer allocations when animating parameters every frame; GPU shader path available for standalone animated sprite views.
- **Costs**: in `Batch.Draw`, active per-sprite effects break out of `DrawAtlas` into one `DrawImageIn` per effected sprite and pay CPU pixel work plus one NSGo pixel-buffer copy/upload on frames where parameters change (`~0.8–13 µs` per 16×16 to 32×32 sprite). URI-only atlases without `Pixels` or `Data` only receive `DrawTintedImageFrom` flash fallback and full-dissolve culling unless rendered via `EffectShader`. `EffectShader` caps `PaletteSwap` at 8 entries (`MaxShaderPaletteEntries`) and draws empty on targets without GPU shaders (HarmonyOS, mini programs).

## Verification

- Pixel-comparison tests in `packages/game/sprite/effect_test.go` (`TestPerSpriteEffectsLeaveNeighborsAndBackgroundUnchanged`) verify 0 mismatched pixels on neighboring sprites and exterior backdrop for flash, silhouette, 1 px and 2 px outlines (including tightly cropped frames adjacent to another atlas frame), partial/full/noise-map dissolve, palette swap, custom hook, and combined stages.
- `TestEffectParametersChangeEveryFrameWithoutLeaks` verifies monotonic pixel updates, monotonic `ImageSource.Version` bumps, unchanged-frame cache reuse, zero `*image.RGBA` reallocations across animated frames, and restoration of `DrawAtlas` when cleared.
- `TestEffectShaderProgramCompilesAndBindsUniforms` compiles the Kage program via `EffectProgram()` and mounts `EffectShader` with `core.AnimatedValue` and signal-driven `UniformsFunc` in `nsgotest`.
- `go run ./cmd/sizecheck -kind wasm -budget ../../benchmarks/web-budget.json` passes (`7.59 MiB` raw / `2.05 MiB` gzipped).
