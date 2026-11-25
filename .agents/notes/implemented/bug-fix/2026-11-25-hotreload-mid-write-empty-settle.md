# Agent Note: Settle 0-byte mid-write reads in hotreload Watcher

Status: implemented

## Problem

`TestShaderHotReloadUpdatesPixelsWithinOneSecondAndRecoversFromErrors` flaked on CI under load with `shaders/quad.wgsl: empty shader source` (#962). The root cause affects both tests and real editors: in-place saves (`os.WriteFile` and editors that save via `O_TRUNC` followed by `write`) briefly leave the target file at 0 bytes. When `hotreload.Watcher` polled during that truncation gap and the writer stalled longer than `DefaultDebounce` (40 ms), `checkEntry` observed two identical 0-byte reads separated by 40 ms and delivered `[]byte{}` as a stable change, surfacing a spurious empty-source `ReloadError` before the real shader bytes arrived.

## Decision

Handle transient 0-byte reads in `hotreload.Watcher` (`packages/game/tooling/hotreload/hotreload.go`) and make the hot-reload pixel test write atomically:

- **Empty-file settle window (`emptyFileSettle = 250ms`)**: When debouncing is enabled (`w.cfg.debounce > 0`), `Watcher.settleDuration(size)` requires a 0-byte file (`size == 0`) to remain unchanged for `max(w.cfg.debounce, 250ms)` before delivery, while non-empty files keep the existing `w.cfg.debounce` (40 ms default) and `WithDebounce(0)` preserves immediate delivery.
- **Fast recovery polling (`considerPending`)**: While a 0-byte pending entry is settling, `considerPending` caps the next poll delay to `w.cfg.debounce` (40 ms default) instead of sleeping for the full 250 ms settle window or `DefaultPollInterval` (150 ms), so the real bytes are picked up on the next debounce tick once the writer finishes.
- **Post-debounce revert guard**: When the final re-read after debounce observes content whose hash matches `entry.delivered.hash` (for example, a truncate followed by writing back identical content), `checkEntry` clears `hasPending` without firing a spurious reload, and updates `entry.delivered.hashedAt = now` on delivery.
- **Atomic test writes and truncate-then-write regression coverage**: `TestShaderHotReloadUpdatesPixelsWithinOneSecondAndRecoversFromErrors` writes shaders via a same-directory temp file and `os.Rename` (`writeFileAtomic`), while `TestShaderHotReloadToleratesTruncateThenWrite` and `TestTruncateThenWriteSettlesBeforeDeliveringEmpty` explicitly hold a truncated file empty past the normal debounce window before writing (verifying zero empty-source errors) and then leave the file genuinely empty (verifying the empty-source error still fires after settling).

## Alternatives considered

### Silently ignore all empty shader reads in `ShaderReloader`

Dropping 0-byte payloads in `ShaderReloader.ReloadShader` or `CompileShaderSource` would prevent empty-source errors on truncate-then-write saves. It was rejected because (1) a user who genuinely clears and saves a shader file would receive no diagnostic, and (2) the same truncate-then-write race also hits `.material.yaml`, sprite atlas JSON, and YAML resource watchers; fixing it in `Watcher` protects every watched asset type while preserving genuine empty-file errors.

### Raise `DefaultDebounce` globally to 250 ms

Increasing `DefaultDebounce` for every file size avoids special-casing `size == 0`. It was rejected because it adds 210 ms of latency to every normal atomic or fast save, eating into the sub-second hot-reload budget (`DefaultPollInterval = 150ms` + debounce) when only 0-byte reads carry the truncate-then-write ambiguity.

### Export a new `WithEmptyDebounce` option

Adding a public `Option` would let callers tune the empty-file settle window. It was rejected to keep the experimental `hotreload` API surface minimal when `emptyFileSettle = 250ms` already sits comfortably between editor truncate gaps and the 1-second reload budget without extra configuration.

## Consequences

- **Benefits**:
  - Truncate-then-write saves of shaders, materials, atlases, and YAML resources no longer emit spurious empty-file errors during transient 0-byte windows.
  - Genuinely empty files still deliver `[]byte{}` and surface `ReloadError` within ~400 ms under default `NewDevWatcher` timings (150 ms poll + 250 ms empty settle).
  - Non-empty edits retain the exact existing ~190 ms default poll+debounce latency, and `TestShaderHotReloadUpdatesPixelsWithinOneSecondAndRecoversFromErrors` is immune to `os.WriteFile` scheduling stalls.
- **Costs & limits**:
  - A deliberate save of a genuinely 0-byte file waits up to 250 ms longer than a non-empty edit before its callback runs.
  - An editor that holds a file truncated at 0 bytes for longer than 250 ms before writing will still report an empty-source error until the bytes are written, at which point the reload recovers automatically.

Related: [2026-11-24-shader-material-hot-reload.md](../feature/2026-11-24-shader-material-hot-reload.md)
