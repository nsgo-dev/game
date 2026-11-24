### Added

- `game/assets`: `PackLimits.MaxPackBytes` caps the archive a `DLCManager` downloads or opens (1088 MiB by default), and `ErrPackTooLarge` reports a pack or a file read from it that passes a limit (#546).

### Changed

- `game/assets`: `ParsePackManifest` refuses fields it does not define, keys given twice in one object, and bytes after the manifest, with the line and a fix (#546).
- `game/assets`: the `fs.FS` that `DLCManager.Fetch` returns is also an `io.Closer`; `Remove` of a pack that is still mounted deletes the cached pack when the last mount is released, by `Close` or by the group `FetchGroup` loaded being reset or mounted again (#546).
- `game/modding`: `Manager.SetGrant` on a loaded mod takes effect at once, removing the mod's panels when it revokes `CapUI` and reloading the mod when the grant changes `CapAudio` or `CapStorage` (#546).

### Fixed

- `game/assets`: a mounted DLC pack reads each file as it decompresses, bounded by `PackLimits`, instead of allocating the size the archive's header claims (#546).
- `game/assets`: a download of a `PackSpec` without a `Size` stops at `MaxPackBytes` instead of growing its `.part` file without bound, and a `206 Partial Content` without `Content-Range` restarts the download instead of appending an unchecked body (#546).
- `game/assets`: a malformed `PackSpec` is refused before the cache is touched, and a cached pack whose bytes match its spec is kept when only its signature or contents are refused, so neither deletes good cached bytes (#546).
- `game/assets`: `MemoryPackCache` holds a pack once instead of copying it up to three times (#546).
- `game/modding`: `Manager.Close` leaves no mod reporting `StateLoaded`, and calling an `onEvent` or `onStep` unsubscribe function after the mod unloaded no longer panics (#546).

### Security

- `game/modding`: a mod without `CapStorage` can keep at most one byte through the `"nsgo"` module's `Storage`, which NSGo exposes in every sandbox: its storage partition gets a one-byte quota (#546).
