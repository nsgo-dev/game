# Agent Note: Remote loose asset delivery via remote.yaml and quota-bounded cache

Status: implemented

## Problem

`assets.Manager` previously resolved `"asset:"` URIs only from packaged app resources (`core.Scope.ReadAsset`), an `fs.FS` (`WithFS`), or a custom `ReadAsset` callback, while network delivery existed only as whole signed ZIP archives through `DLCManager`. Games targeting mini-game runtimes (which enforce strict main-package caps and a 200 MB local filesystem quota) or web CDNs need to host loose files (music tracks, per-level art, data tables) under immutable content-hashed URLs, cache verified copies locally under a strict byte budget, and plug in non-`net/http` transports once NSGo exposes mini-program host requests.

## Decision

Loose remote asset delivery is split across a versioned YAML manifest (`remote.yaml`), an on-device quota-bounded cache (`RemoteCache` / `DirRemoteCache` / `MemoryRemoteCache`), a `WithRemote` `Manager` option with a `Fetch` transport seam, and an `assets.remote` publishing step in `game assets`:

- **Manifest format (`remote.yaml`, `assets.RemoteManifestVersion = 1`)**: Strict YAML (`KnownFields(true)`, single document, sorted by `uri`, unknown fields and duplicate normalized URIs refused) recording optional `base` URL and `files` entries (`uri`, content-hashed `path` of the form `<name>.<hash8>.<ext>`, exact `size`, lowercase 64-char hex `sha256`).
- **Build pipeline (`project.Assets.Remote` / `assetbuild.KindRemote`)**: `game assets` copies candidate assets (from `remote.in`, else `manifest.in`, else built target outputs; optionally filtered by `remote.include`) to `<remote.out>/<name>.<hash8>.<ext>` and writes `<remote.out>/remote.yaml`. Outputs are tracked in `.game/assets-cache.yaml` so unchanged runs are no-ops, `-check` validates freshness, and superseded hashed files are removed automatically.
- **Runtime loading (`assets.WithRemote`)**: URIs listed in `RemoteManifest` (across bytes, custom loaders, images, atlas JSON and atlas images, and sounds) are resolved from `RemoteCache` first and fetched on cache miss through `Fetch func(ctx context.Context, url string) (io.ReadCloser, int64, error)` (defaulting to `net/http`). Unlisted URIs fall through unchanged to local readers (`WithReadAsset`, `WithFS`, or `core.Scope.ReadAsset`).
- **Integrity, deduplication, and cancellation**: Every cache read and network download is verified against exact `Size` (`ErrSizeMismatch`) and constant-time `SHA256` (`ErrHashMismatch`); corrupt cache blobs are purged and re-fetched, and tampered downloads never enter the cache. Concurrent loads for the same `SHA256` share one in-flight fetch whose context is cancelled only when the last waiter cancels.
- **Quota-bounded LRU cache (`DirRemoteCache`)**: Content-addressed by `SHA256` under `<dir>/files/<sha256>` with atomic temp-file writes and a small versioned text index (`index.yaml`) recording sizes and monotonic access ticks. Default `Quota` is 200 MiB (`DefaultRemoteCacheQuota`). Loading a `Group` pins all referenced remote hashes before starting fetches and unpins them on failure, `Release`, `Reset`, or manager disposal, so LRU eviction never removes a file belonging to a loaded group. `Usage()` and `Clear()` expose cache footprint for settings UIs.

## Alternatives considered

- **Serve loose remote assets as single-file DLC packs through `DLCManager`**: rejected because ZIP framing, Ed25519 signing envelopes, and Range-resume staging add overhead per file and cannot leverage immutable content-hashed CDN caching or a shared cross-group LRU quota.
- **Key `DirRemoteCache` by logical URI instead of `SHA256`**: rejected because content addressing naturally deduplicates identical blobs across URIs, avoids stale-version ambiguity when `remote.yaml` updates, and makes cache hits purely a hash check without conditional HTTP requests.
- **Store cache metadata in SQLite or a binary index**: rejected to follow the repository's plain-text format rule (`AGENTS.md`) and keep `pkg/assets` dependency-light and portable across every target including `js/wasm`.

## Consequences

- **Benefits**: Games stay within mini-game and web package caps while serving immutable CDN assets on demand; offline restarts work from verified disk caches; loaded groups are immune to mid-scene eviction; mini-program hosts can inject a custom `Fetch` without depending on `net/http`.
- **Costs and known limits**: Per-group `delivery.remote` assignment in `game.yaml` export presets awaits #1735 (currently `assets.remote.include` filters published paths), and mini-program host `Fetch` wiring awaits `nsgo-dev/nsgo#2671`. On targets without local disk storage (such as the web), callers use `MemoryRemoteCache` (the default when `Cache` is nil) rather than `DirRemoteCache`.
