# cloudsave

`github.com/nsgo-dev/game/pkg/online/cloudsave` is a self-hostable cloud save service: each player account has save slots over HTTPS, kept in versions. Two devices writing the same slot at once cannot overwrite each other, a slot over the size limit is refused with a clear error, and the last few versions of each slot can be listed and restored. Games talk to it through `liveops.CloudClient`, a `liveops.CloudSync`, so a `liveops.CloudSyncer` keeps local saves in step with it and resolves conflicts by its policy.

The service is server-side code in its own packages; a game client imports only `liveops`. It keeps no accounts of its own: it verifies the tokens of the [account service](accounts.md) offline with `accounts/authtoken`, and files each player's slots under the token's subject, the account ID. A player signed in to the same account on the web and on an Android phone reads and writes the same slots.

| Package | What it holds |
| --- | --- |
| `cloudsave` | `Service`, `Config`, the `Store` interface, `MemoryStore` and the HTTP `Handler` |
| `cloudsave/sqlstore` | A `Store` in SQLite (`OpenSQLite`, pure Go, every target but the web) or Postgres (`OpenPostgres`, shared by several service processes) |
| `cloudsave/storetest` | The `Store` contract tests, for a store of your own |
| `cmd/cloudsaved` | A standalone HTTP server |
| `liveops` | `CloudClient`, the game's side |

## On the device

```go
cloud, err := liveops.NewCloudClient(liveops.CloudClientOptions{
	URL: "https://saves.example.com",
	Token: func(ctx context.Context) (string, error) {
		return session.Token(ctx) // the account service's token, refreshed when it expires
	},
})
if err != nil {
	return err
}
syncer, err := liveops.NewCloudSyncer(liveops.CloudSyncOptions{
	Local:       save.StorageBackend(cx.Storage(), "mygame"),
	Cloud:       cloud,
	State:       cx.Storage(),
	Device:      deviceID,
	SaveVersion: 2, // the Store.Version this build writes
	Policy:      liveops.PolicyKeepBoth,
})
```

Save through `save.NewStore(syncer.Backend(), save.WithVersion(2), ...)` and call `syncer.Sync(ctx)` when the game starts, after a save and when it returns to the foreground; see [liveops](liveops.md#cloud-saves) for how the syncer detects and resolves conflicts.

## Mixed client versions

When a player plays on a newer build (for example save version 3) and then opens an older build (save version 2), setting `CloudSyncOptions.SaveVersion` to the build's `save.Store.Version` keeps the older client from overwriting either side:

- Before writing a cloud copy locally, `CloudSyncer` checks its save envelope version (`save.EnvelopeVersion`). When it exceeds `SaveVersion`, the syncer leaves the local slot and sync state untouched and reports `liveops.OutcomeNeedsUpdate` with `SlotResult.LocalVersion` and `SlotResult.CloudVersion` (collected by `res.NeedsUpdate()`), so the game can prompt the player to update while keeping the local save loadable.
- If the player keeps playing or starts fresh on the older build, the next sync sees both sides diverged and never uploads over the newer-version cloud copy.
- In conflicts where both copies carry different envelope versions, `CloudSyncer` never resolves in favour of the older envelope version; it falls back to keeping both (and when the cloud copy still exceeds `SaveVersion`, forks the local copy without overwriting the local slot and reports `OutcomeNeedsUpdate`). `Conflict.Local.EnvelopeVersion` and `Conflict.Cloud.EnvelopeVersion` expose both versions so `PolicyAsk` dialogs can tell the player a copy was saved by a newer game version.

```go
res, err := syncer.Sync(ctx)
if err != nil {
	return err
}
for _, slot := range res.NeedsUpdate() {
	showUpdatePrompt(slot.Slot, slot.LocalVersion, slot.CloudVersion)
}
```

`CloudClient` needs an `https` URL; `http` is accepted only on a loopback host (`localhost`, `127.0.0.1`, `::1`) for development. It uses `net/http`, so it works on every target: on the web its requests go through the browser's `fetch`, and the page's origin must be in the service's `allowedOrigins`; an Android app needs the `INTERNET` permission. Its errors wrap the `liveops` sentinels:

| Error | When |
| --- | --- |
| `ErrSlotNotFound` | The slot, or the version asked for, does not exist |
| `ErrCloudChanged` | A conditional write lost to another device's; nothing was written |
| `ErrSlotTooLarge` | The payload is over the service's `maxSlotBytes`; the message gives the limit |
| `ErrTooManySlots` | A new slot would exceed the service's `maxSlots` |
| `ErrCloudUnavailable` | The network or the service failed, the token source failed or the service refused the token; a sync stops on it as it does when offline |

A done context's error is returned as it is.

## Concurrent writes

Every write names the payload it replaces. `CloudClient` implements `liveops.ConditionalCloudSync`: `SaveIf(ctx, slot, data, match)` writes only while the slot holds the payload whose `liveops.PayloadHash` is `match` (`"sha256:"` and the hex SHA-256 of the raw bytes), or does not exist when `match` is empty, and `DeleteIf` deletes on the same condition. Over HTTP the condition is `If-Match: "<hash>"` or `If-None-Match: *`; the service checks and writes in one atomic step, and answers `412 changed` when the slot holds anything else.

`CloudSyncer` makes every cloud write conditional on the copy it read. When the phone and the browser both changed a slot and the browser uploads first, the phone's upload fails with `ErrCloudChanged` and changes nothing; on its next sync the phone sees both copies as a conflict, and the policy keeps the newer, keeps both as a fork slot, or asks the player. No upload is ever overwritten silently.

## Versions and restore

Every write or delete adds a version to the slot, numbered from 1, and the service keeps the newest `keepVersions` of them. A delete is a version that marks the slot deleted, so a deleted slot keeps its history and can be brought back.

```go
versions, err := cloud.Versions(ctx, "main") // newest first: Number, Deleted, Hash, Size, Created
data, err := cloud.LoadVersion(ctx, "main", versions[2].Number)
err = cloud.Restore(ctx, "main", versions[2].Number)
err = cloud.Purge(ctx, "main") // the slot and its history, for good
```

`Restore` writes the old copy as a new version, conditional on the slot still holding the copy it read (`ErrCloudChanged` otherwise); restoring a deletion deletes the slot. A payload written by `CloudSyncer` is re-stamped with the next revision and the current time, so every device takes the restore as the newest copy: a device whose copy has not changed since its last sync downloads it, and one with unsynced changes gets a conflict its policy resolves. Other payloads are restored byte for byte. `Service.Restore` on the server restores the bytes unchanged.

## Limits

- `maxSlotBytes` bounds a payload. A larger one is refused with `413 slot_too_large` and `{"limit": maxSlotBytes}` before the service reads past the limit, and the slot keeps its current copy. A `CloudSyncer` payload is the save inside a JSON envelope: about 250 bytes more, with the save's quotes and backslashes escaped, or base64, a third larger, for data that is not UTF-8.
- `maxSlots` bounds an account's live slots; a write that would create one more is refused with `409 too_many_slots`. Fork slots made by `PolicyKeepBoth` count. A deleted slot does not count, but once an account has more than `maxSlots` deleted slots the oldest are purged with their history, so an account never holds more than twice `maxSlots` slots.
- Slot names are 1 to 200 bytes of UTF-8 without control characters or `/`, other than `.` and `..` (`ValidateSlot`); every name `save.ValidateSlot` accepts passes. In URLs they are path segments, escaped as `url.PathEscape` does.

## Configuration

`Config` is a versioned YAML file:

```yaml
version: 1
maxSlotBytes: 1048576
maxSlots: 32
keepVersions: 5
allowedOrigins:
  - https://play.example.com
```

| Field | Meaning |
| --- | --- |
| `version` | The schema version, `1` |
| `maxSlotBytes` | The largest payload, from 1 byte to 64 MiB |
| `maxSlots` | Live slots per account, from 1 to 10000 |
| `keepVersions` | Versions kept per slot, the current one included, from 1 to 100 |
| `allowedOrigins` | Web origins whose pages may call the service; omitted when empty |

`ParseConfig` refuses unknown fields, trailing documents, out-of-range values and an origin with a path, a wildcard, a port outside 1 to 65535 or a scheme other than `http` and `https`. `Config.Marshal` writes the canonical form: the fields in the order above and `allowedOrigins` only when it has entries. A file in that form, such as one `Marshal` or `cloudsaved init` wrote, reads back and writes again unchanged. `DefaultConfig` returns the settings shown, without origins.

## Storage

`Store` keeps the versions. Its `Append` is atomic: of two writes based on the same version, at most one commits.

```go
store, err := sqlstore.OpenSQLite(ctx, "saves.db")                     // one service process
store, err := sqlstore.OpenPostgres(ctx, os.Getenv("CLOUDSAVE_POSTGRES_DSN")) // several processes
svc, err := cloudsave.New(cfg, cloudsave.Options{Store: store, Tokens: verifier, Logger: slog.Default()})
```

The SQL store keeps two tables, `cloudsave_slots` (each slot's newest version) and `cloudsave_versions` (the kept versions with their payloads), and versions its schema in `cloudsave_schema`. Each write compares and sets the slot's newest version number in one transaction; on Postgres it also takes an advisory lock on the account, so the slot count and pruning see every concurrent write. SQLite uses the pure-Go `modernc.org/sqlite` driver with write-ahead logging and one connection; Postgres uses `github.com/jackc/pgx/v5`. Both create their tables when missing and refuse a schema newer than they know. `NewMemoryStore` keeps everything in memory for tests and prototypes, and a store of your own passes `storetest.Run`.

## HTTP

`Service.Handler` serves the service. Every request except `GET /v1/openapi.yaml` carries `Authorization: Bearer <token>` with a token from the account service, and every response carries `Cache-Control: no-store`. Payloads are `application/octet-stream`; a version is `{"slot", "version", "deleted", "hash", "size", "created"}`, `created` in RFC 3339 with milliseconds and `deleted` and `hash` only when set. The full machine-readable contract is published as an [OpenAPI 3.1 description](https://game.nsgo.dev/schemas/cloudsave-openapi.v1.yaml) and served live at `GET /v1/openapi.yaml`.

| Request | Answer |
| --- | --- |
| `GET /v1/openapi.yaml` | the OpenAPI 3.1 description in YAML (unauthenticated; CORS allowed for `allowedOrigins`) |
| `GET /v1/saves` | `{"slots": [version]}`, the live slots by name |
| `GET /v1/saves/{slot}` | the payload, with `ETag` (its hash in quotes) and `Save-Version` |
| `PUT /v1/saves/{slot}` | version; `If-Match: "<hash>"` or `If-None-Match: *` make it conditional |
| `DELETE /v1/saves/{slot}` | 204; the same headers make it conditional |
| `GET /v1/saves/{slot}/versions` | `{"versions": [version]}`, newest first |
| `GET /v1/saves/{slot}/versions/{n}` | the payload of version `n` |
| `DELETE /v1/saves/{slot}/versions` | 204, the slot and its history purged |

Errors are `{"error": code}`: `bad_request` 400 (an invalid slot name, version number or precondition header), `invalid_token` 401, `not_found` 404, `too_many_slots` 409, `deleted` 410 (a version that is a deletion has no payload), `changed` 412, `slot_too_large` 413 with `limit`, `unavailable` 503 when the request was cancelled or timed out, and `internal` 500. For `allowedOrigins` the handler answers CORS preflight requests and exposes `ETag` and `Save-Version`. It does not limit request rates: run it behind TLS and a rate limiter.

## cloudsaved

`cmd/cloudsaved` runs the service:

```sh
cloudsaved init  -config cloudsave.yaml
cloudsaved serve -config cloudsave.yaml -keyset keys.yaml -sqlite saves.db -addr :8081
CLOUDSAVE_POSTGRES_DSN=postgres://... cloudsaved serve -config cloudsave.yaml -keyset keys.yaml -postgres
```

`init` never overwrites a configuration. `-keyset` is the key set file `accountsd genkey` keeps; `serve` reads it once at start, so restart it after a key rotation adds a key. `serve` logs JSON to stderr and stops on SIGINT or SIGTERM.
