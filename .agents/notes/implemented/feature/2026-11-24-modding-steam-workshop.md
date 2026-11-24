# Agent Note: Steam Workshop integration for game/modding

Status: implemented

## Problem

`packages/game/modding` loads sandboxed TSX/JS mods from local directories via `Manager.Discover` and `Manager.DiscoverFS`, but games shipping on Steam had no way to publish mods or custom levels to Steam Workshop, track the assigned `PublishedFileID` across updates, or sync a player's Steam Workshop subscriptions directly into `Manager` without hand-written CGo bindings.

## Decision

`packages/game/modding` integrates Steam Workshop through four pieces:

1. **Dynamic mod registration on `Manager`**: `Manager.Install`, `Manager.InstallFS`, and `Manager.Uninstall` validate manifests and topological dependency order against already-registered mods so Workshop items can coexist with local mods and be added, updated, or removed at runtime without replacing the whole registry.
2. **Backend abstraction (`WorkshopBackend`) and `Workshop` coordinator**: `Workshop` wraps a `WorkshopBackend` and `*Manager`.
   - `Publish` validates `mod.yaml`, the entry script, `workshop.yaml`, preview path confinement, and symlink safety before creating or updating a Workshop item. It persists the assigned `PublishedFileID` immediately after `CreateItem` so transient upload failures never orphan duplicate items on retry. When Steamworks reports `m_bUserNeedsToAcceptWorkshopLegalAgreement`, `Publish` persists `workshop.yaml` and returns the populated `PublishResult` (including `LegalAgreementURL`) together with `ErrWorkshopLegalAgreementRequired`.
   - `SyncSubscribed` removes unsubscribed items from `Manager`, downloads missing or outdated items, enforces path confinement (`WorkshopOptions.AllowedRoots` and no symlinks escaping the mod directory, failing with `ErrWorkshopUnsafePath`), and installs valid items in topological dependency order while isolating broken sibling items.
3. **Deterministic `workshop.yaml` sidecar (`WorkshopMetadata`)**: Stored beside `mod.yaml` with `version: 1`, `KnownFields(true)`, strict literal version checks, sorted flow-style tags, fixed key order, and no timestamps or volatile fields so load/save round trips are byte-for-byte stable.
4. **Pure-Go Steamworks `ISteamUGC` adapter (`NewSteamWorkshop`) plus `NewMemoryWorkshop`**:
   - Desktop targets (`darwin && !ios`, `linux && !android`, `windows`) load `libsteam_api.dylib` / `libsteam_api.so` / `steam_api64.dll` via `purego` (Unix `purego.Dlopen`/`Dlsym`) or `syscall.LoadDLL` (Windows) and bind the Steamworks flat C API (`steam_api_flat.h`) with `purego.RegisterFunc`.
   - Async `SteamAPICall_t` handles are awaited via `SteamAPI_RunCallbacks`, `SteamAPI_ISteamUtils_IsAPICallCompleted`, and `SteamAPI_ISteamUtils_GetAPICallResult`, decoding callback structs from raw little-endian buffers using explicit platform struct packing (`pack = 4` on macOS/Linux for `VALVE_CALLBACK_PACK_SMALL`, `pack = 8` on Windows for `VALVE_CALLBACK_PACK_LARGE`).
   - Missing libraries, stopped Steam clients, and unsupported targets (`ios`, `android`, `js/wasm`) return `ErrWorkshopUnavailable`.
   - `MemoryWorkshop` provides a multi-user filesystem-backed `WorkshopBackend` (`AsUser`) for deterministic end-to-end tests and offline workflows.

## Alternatives considered

- **CGo wrapper around Steamworks C++ headers**: Would directly use `ISteamUGC` C++ classes and `CCallResult<T>`, avoiding manual callback offset decoding. Rejected because repository rules forbid hand-written C/CGo and require pure Go (`purego`/`syscall`) so cross-compilation and `nsgo check` work across all targets without native toolchains or Steamworks SDK headers at build time.
- **Embedding Workshop metadata inside `mod.yaml`**: Would keep one YAML file per mod instead of adding `workshop.yaml`. Rejected because `mod.yaml` is the engine-agnostic mod manifest shared across local installs, direct downloads, and non-Steam storefronts; coupling Steam-specific fields (`appId`, `publishedFileId`, `visibility`, `preview`) into `mod.yaml` would pollute non-Steam mods and break strict unknown-field checks for existing manifests.
- **Storing Steamworks callback results directly in Go structs**: Simpler than reading byte offsets with `encoding/binary`. Rejected because Steamworks uses `#pragma pack(4)` on macOS and Linux (`VALVE_CALLBACK_PACK_SMALL`), which disagrees with Go's natural 8-byte alignment for `uint64` fields after a 4-byte `EResult` and would silently misread `PublishedFileId_t`.

## Consequences

- **Benefits**: Zero CGo, builds and vets cleanly across macOS, Windows, Linux, Android, and Web; end-to-end publish-and-subscribe flows are fully verified in unit tests via `MemoryWorkshop` without needing a live Steam client; broken or unsafe Workshop items cannot crash the host or escape allowed directories.
- **Costs**: The `purego` adapter relies on Steamworks flat C API symbol names and `ISteamUGC` callback struct layouts; future Steamworks SDK versions that add newer `SteamAPI_SteamUGC_v0XX` accessors or revise `SteamUGCDetails_t` offsets will require adding the new accessor name or updating the decoder offsets.
- **Verification**: Covered by `packages/game/modding/workshop_test.go` (sample `boss-rush` mod publish/re-publish/subscribe/update/unsubscribe lifecycle, invalid manifest rejection, legal agreement reporting, dependency ordering and fault isolation, path confinement, strict YAML round-trip, unavailable Steam fallback) and `packages/game/modding/workshop_steam_internal_test.go` (pack-4 and pack-8 binary callback decoders).
