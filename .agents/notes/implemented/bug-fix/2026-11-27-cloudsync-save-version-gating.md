# Agent Note: Save envelope version gating in `CloudSyncer`

Status: implemented

## Problem

`save.Store` versions each slot's JSON envelope (`{"version": N, "data": ...}`) and refuses to load a save newer than `Store.Version` with `save.ErrFutureVersion` without falling back to the backup. Previously, `liveops.CloudSyncer` ignored the save envelope's version completely:

1. When a newer client (save version 3) uploaded a slot, an older client (save version 2) saw a one-sided cloud change and unconditionally overwrote its local v2 primary with the v3 envelope.
2. `Store.Load` on the older client then failed with `ErrFutureVersion`. A game that reacted by starting fresh and saving rotated the v3 copy into the local backup, discarding the local v2 progress.
3. The next sync saw only a local change and uploaded the fresh v2 save over the cloud's v3 progress.
4. Under `PolicyNewest`, an older client playing offline with a later timestamp also overwrote a newer-schema cloud save during conflict resolution.

## Decision

`pkg/save` and `pkg/online/liveops` share one envelope-version reader and enforce monotonic schema safety in `CloudSyncer`:

- **`save.EnvelopeVersion(payload []byte) (int, bool)` (`pkg/save/save.go`)** extracts the positive integer `"version"` from a save envelope, accepting JSON objects with unknown fields so saves written by newer builds with extra envelope fields still report their version. `decodeEnvelopeData` uses the same helper so `Store.Load` and `CloudSyncer` agree on every payload.
- **`CloudSyncOptions.SaveVersion` (`pkg/online/liveops/cloudsync.go`)** configures the game's supported `save.Store.Version` (0 disables version gating against the client's version; negative values are rejected by `NewCloudSyncer`).
- **`SaveVersion.EnvelopeVersion`** is populated on both `readLocal` and `DecodeCloudSave` (for both `CloudSaveFormat` envelopes and plain cloud payloads) without changing the cloud JSON wire format. Both sides' envelope versions appear on `Conflict.Local` and `Conflict.Cloud` so `PolicyAsk` dialogs can explain when a copy came from a newer game version.
- **`OutcomeNeedsUpdate` (`"needs-update"`)**, `SlotResult.LocalVersion`, `SlotResult.CloudVersion`, and `SyncResult.NeedsUpdate()` report when a cloud copy's envelope version exceeds `CloudSyncOptions.SaveVersion`. `applyCloud` refuses to write such a cloud copy to the local backend or advance sync state, leaving the older client's local save loadable and ensuring subsequent local writes diverge into a conflict rather than a one-sided upload. A downgraded client whose sync base still matches a newer-version cloud copy (`ch == st.Hash`) likewise refuses to delete or overwrite the cloud copy.
- **Mixed-version conflict guard in `syncOp.apply`**: whenever a conflict would resolve in favour of a copy whose `EnvelopeVersion` is strictly older than the other side's (or overwrite/delete a cloud copy exceeding `SaveVersion`), `apply` falls back to `ChoiceBoth` (or `ChoiceCloud` when local was deleted). When `fork` runs while the cloud copy exceeds `SaveVersion`, it preserves the local copy in `<slot>.conflict-<device>` on both sides, leaves both the local and cloud slots untouched, and returns `OutcomeNeedsUpdate` with `Fork` set; repeated syncs reuse the existing fork without duplication, and upgrading the client later converges cleanly to `OutcomeKeptBoth`.

## Alternatives considered

- **Storing the game's save version in the cloud wrapper (`cloudEnvelope`)** — Would expose the version without parsing `data`, and could work for non-JSON payloads. Rejected because `CloudSyncer` already wraps `save.Store` JSON envelopes whose version is authoritative inside `data`, adding a new field to `cloudEnvelope` would break strict `DisallowUnknownFields()` decoding on existing older clients (turning newer saves into `ErrCorruptCloudSave`), and plain/legacy cloud payloads without `CloudSaveFormat` still carry the save envelope directly.
- **Returning an error (e.g. `ErrFutureVersion`) from `SyncSlot` instead of `OutcomeNeedsUpdate`** — Would reuse `save`'s error path and skip the slot via `Sync`'s error join. Rejected because needing an app update is a normal player-facing state during staged store rollouts rather than a storage fault, callers need both `LocalVersion` and `CloudVersion` structurally to render an update prompt, and a conflict that forks an older local copy while preserving cloud v3 still produces a meaningful `SlotResult` (`Conflict`, `Fork`, versions).
- **Skipping fork creation altogether when a mixed-version conflict occurs on an older client** — Simpler and avoids creating a fork slot until the older client updates. Rejected because offline progress made on an older device (for example a tablet not yet updated) would remain trapped locally and invisible to updated devices until the older device updates; forking the local copy to `<slot>.conflict-<device>` preserves both copies in the cloud while keeping local and cloud primary slots intact and idempotent across retries.

## Consequences

- **Benefits**: Older clients with `CloudSyncOptions.SaveVersion` set keep their local saves loadable when a newer build uploads, never clobber newer-version cloud saves on subsequent syncs or under `PolicyNewest`, require zero wire-format changes to `cloudEnvelope`, and preserve offline progress from mixed-version conflicts via reusable fork slots.
- **Costs and known limits**: Games that want update-required gating must set `CloudSyncOptions.SaveVersion` to match `save.Store.Version` (the zero value leaves `SaveVersion` gating off for backwards compatibility with existing callers and non-envelope payloads, though mixed-version conflicts still refuse to resolve in favour of an older envelope version). If a game stores non-`save.Store` JSON payloads that happen to contain a positive integer `"version"` field, leaving `SaveVersion: 0` avoids unintended gating.

## Verification

- `go tool nsgo test ./pkg/save/... ./pkg/online/liveops/... ./pkg/online/cloudsave/...` covers `TestEnvelopeVersion` in `pkg/save/save_test.go`, the full four-step reproduction and upgrade convergence in `TestCloudSyncNewerVersionPreservesLocalAndCloud`, future-field envelopes and downgraded-client protection in `TestCloudSyncNewerVersionEdgeCases`, and `PolicyNewest`/`PolicyAsk`/`Resolve` mixed-version conflicts in `TestCloudSyncMixedVersionConflicts` in `pkg/online/liveops/cloudsync_test.go`.
- `go tool nsgo check -target macos_arm64,linux_x64,windows_x64 -jobs 2 ./pkg/save ./pkg/online/liveops ./pkg/online/cloudsave` vets the modified packages across desktop targets.
