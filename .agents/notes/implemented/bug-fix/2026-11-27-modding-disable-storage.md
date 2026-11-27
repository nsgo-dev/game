# Agent Note: Withhold mod Storage with tsx.Sandbox.DisableStorage

Status: implemented

## Problem

In `pkg/modding`, a mod without `CapStorage` was previously restricted from NSGo's `"nsgo"` module `Storage` only by shrinking `tsx.Limits.Storage` to `noStorageQuota = 1` byte (`pkg/modding/manager.go`). That workaround leaked in two ways: a mod without `CapStorage` could still persist one entry whose key and value totaled one byte (such as `setItem("a", "")`), and it could still read any keys already stored in its partition (for example data written while it previously held `CapStorage`). Now that `github.com/nsgo-dev/nsgo` v0.3.4 provides `tsx.Sandbox.DisableStorage` (nsgo-dev/nsgo#2299), the workaround is obsolete and weakens script-mod capability enforcement.

## Decision

`Manager.loadSingle` in `pkg/modding/manager.go` removes `noStorageQuota` and the `limits.Storage` override, and sets `DisableStorage: !effCaps.Has(CapStorage)` on `tsx.Sandbox` while keeping `Storage: m.sandboxStoragePartition(mod.manifest.ID)` and the host's `ManagerOptions.Limits` intact.

- Every `"nsgo"` `Storage` operation (`getItem`, `setItem`, `removeItem`, `clear`, `keys`) from a mod without `CapStorage` rejects with `tsx: Storage is disabled for this sandbox`, including one-byte writes and reads of previously written keys.
- `CapStorage` remains in `sandboxCapabilities`, so `Manager.SetGrant` rebuilds the sandbox whenever `CapStorage` is revoked or restored on a loaded mod.
- Existing partition data is preserved when `DisableStorage` is enabled, so restoring `CapStorage` later makes previously stored entries readable again.
- Web behavior is unchanged because `loadSingle` still refuses mods on web targets without `ManagerOptions.WebUnisolated` (`ErrWebUnisolated`) and forwards `WebUnisolated` to `tsx.Sandbox`.

## Alternatives considered

- **Keep the one-byte `noStorageQuota` fallback alongside `DisableStorage`**: unnecessary because `go.mod` requires `github.com/nsgo-dev/nsgo v0.3.4` and keeping the `limits.Storage` override would only obscure how `CapStorage` is enforced.
- **Clear the mod's storage partition when `CapStorage` is revoked**: would cause destructive data loss when a player or host temporarily toggles a mod's `storage` grant off and back on. Leaving partition data in place matches `tsx.Sandbox.DisableStorage`'s contract.

## Consequences

- **Benefits**: `CapStorage` is enforced cleanly with no one-byte write leak or stale-data read leak, `ManagerOptions.Limits.Storage` is no longer overwritten, and `noStorageQuota` is gone.
- **Costs and known limits**: On web targets where `ManagerOptions.WebUnisolated` is opted in, `DisableStorage` still only withholds NSGo's `Storage` module and cannot block direct browser `localStorage` access, as documented in `docs/en/modding.md` and `docs/zh/modding.md`.
