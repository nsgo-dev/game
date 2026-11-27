# Agent Note: pkg/online/audit append-only JSON Lines audit log

Status: implemented

## Problem

Nothing on the server side kept a durable record of who did what: `liveops.Analytics` is client-side, consent-gated gameplay analytics, and the daemons log operational events through `slog` to stderr, which rotation mixes with debug output. Kicks (#1313), replaced sessions (#1324) and operator commands (#1328) need an append-only trail with the actor and the subject of each action, greppable and shippable to a log store, with no database. Issue #1329, part of #1312.

## Decision

`pkg/online/audit` (in the `pkg/online` group: only `pkg/online/server`, the daemons and the examples will import it) writes one `<yyyy-mm-dd>.jsonl` file per UTC day under `Options.Dir`, opened `O_APPEND` and never rewritten. Each line is one `Record` — `v` (schema version, `SchemaVersion` 1), `time` (UTC, RFC 3339 with milliseconds), `actor`, `action`, `subject`, optional `data` — encoded through an internal `line` struct so the keys come in a fixed order and the same records always give the same bytes. `Log` fills in `v`, the current time and an empty actor as `"system"`; a record lands in the file of its own UTC day, so a replayed batch lands in the files of the days it happened, and the roll to a new file happens lazily on the first record of a new day rather than on a timer.

`Log` never blocks the caller: records buffer in a channel of `Queue` (4096 by default) and a full queue drops and counts, with `Dropped()` exported for `/metrics` (#1331). One background goroutine appends and fsyncs once per drained burst (it skips the sync while more records wait in the queue, so a burst costs one fsync); `Close` — called from a drain like #1327's — flushes the queue, fsyncs, closes the file and returns the first append failure, while the goroutine keeps appending what it can after an error. `Log` and `Close` synchronize with an `RWMutex` (read for the non-blocking send, write to close the channel) so a `Log` racing `Close` drops instead of panicking on a closed channel. `Retain` deletes files named `<date>.jsonl` older than that many days, counting back from the day being written, when a new day's file opens; other files in the directory are left alone.

The tests replace the clock and the file sync through an unexported `open` (`FileStore`'s replaceable-hooks precedent), which makes the UTC-midnight roll and the full-queue drop deterministic. The format has its `online/audit` entry on `docs/{en,zh}/formats.md` with a strict per-line reader in `internal/repocheck/formats_test.go` (`codeFormats`, `formatVersions` for `SchemaVersion`), and the guide section lives in `docs/{en,zh}/server.md` rather than a page of its own, as the issue asks.

## Alternatives considered

- **Wiring the callers now** (kicks in `pkg/online/server`, the cloudsave `DELETE` routes): neither #1313 nor #1324/#1328 has landed on main, and the issue marks them as later dependencies, so the package ships standalone and the call sites arrive with those issues.
- **Rolling on a timer at UTC midnight**: a timer would create empty files on quiet days and needs the process alive at midnight; keying the file to each record's own UTC day rolls exactly when the first record of a day arrives and lets replayed history land in the right files.
- **An fsync per record**: durable but a kick burst would pay a disk flush per line; the drained-burst sync keeps one fsync per burst while `Close` still guarantees the rest.
- **A blocking `Log` that applies backpressure**: an audit trail must never stall the simulation tick; bounded drop with a visible counter matches the relay queues' drop-and-count policy.
- **Exporting a record reader**: plain `encoding/json` reads the lines, so a `ParseRecord` would be surface without a caller; the strictness check lives in the repocheck test instead.

## Consequences

- **Benefits**: operators get a durable, text, diffable trail that matches the repository's every-file-is-text rule; the writer adds no goroutine or dependency to callers beyond one channel send; drops from a full queue are countable and will be visible on `/metrics`.
- **Costs**: a machine power loss can take the last unsynced burst (the fsync batches by design, as `selfhost.FileStore`'s non-sync mode does); records dropped on append failure are counted but not retried; until #1313/#1324/#1328 land, nothing writes the log yet. Retention counts days from the day being written, so a log that only ever replays old records expires files relative to those old days, not the wall clock.
- **Verification**: `go tool nsgo check`, `go tool nsgo test ./pkg/online/audit/` (11 tests: exact line bytes, defaults, byte stability, midnight roll with a fake clock, deterministic full-queue drop, retention scope, close flush, log-after-close, append-failure reporting, options validation), `go tool nsgo test ./internal/repocheck` (formats page, stability table, skill package list), `go run ./tools/changelog check`.
