### Added

- `game/cloudsave`: a self-hosted cloud save service for the players of the account service, with per-account slots over HTTP, conditional writes (`If-Match`, `If-None-Match`) that refuse a write racing another device, a slot size limit (`413 slot_too_large` with the limit), a slot count limit, the last few versions of each slot kept for listing and restore, SQLite and Postgres stores in `cloudsave/sqlstore`, contract tests in `cloudsave/storetest` and the standalone server `cloudsaved` (#557).
- `game/liveops`: `CloudClient`, the HTTPS `CloudSync` for `cloudsave`, with `Versions`, `LoadVersion`, `Restore` and `Purge`; `ConditionalCloudSync` with `SaveIf`, `DeleteIf` and `PayloadHash`; and `ErrSlotTooLarge` and `ErrTooManySlots` (#557).

### Changed

- `game/liveops`: `CloudSyncer` makes every cloud write conditional when its `Cloud` implements `ConditionalCloudSync`, so an upload by another device is never overwritten: the losing write fails with `ErrCloudChanged` and the next sync resolves the conflict by policy (#557).
