# Agent Note: save slot headers in the envelope

Status: implemented

## Problem

A load screen shows a chapter, a location or a play time for each save slot, but `save.Store.Slots` returned only names, so a game had to `Load` every slot in full or keep a hand-written index slot that could drift from the saves. The envelope parser (`checkEnvelopeKeys` in `pkg/save/save.go`) refused any key besides `version` and `data`, so a header could not be added without an envelope change. Issue #1733, part of #1479.

## Decision

The envelope gains an optional `header` key: `{"version": N, "header": {...}, "data": ...}`. `parseEnvelope` accepts it at most once and only as a JSON object; a duplicate, `null` or non-object header is `ErrCorrupt`, as any other malformed envelope is. `Save` leaves the key out (`omitempty`), so envelopes it writes are byte-for-byte what they were. `Store.SaveWithHeader(slot, header, value)` refuses a header that does not encode to an object before touching the backend, and shares `Store.write` with `Save`.

`Store.Headers()` returns `map[string]json.RawMessage` for the slots whose envelope has a header. It parses each slot's envelope with `parseEnvelope` into `json.RawMessage` fields and never unmarshals the data into a Go value. It reads the copy `Load` would: the primary, or the backup when the primary cannot be read or parsed, so after a torn write the listed header belongs to the data `Load` returns. A slot with no valid copy is left out, and `Load` reports why. Because the backends keep only a valid envelope as the backup (`isValidEnvelope`), header envelopes are backed up like any other.

## Alternatives considered

- **A sidecar header copy per slot** (the issue's proposal): listing would read only small files, but the header and the data become two writes in each backend, and keeping them consistent across a crash between them needs a second atomic protocol for both `FileBackend` and `StorageBackend`. One envelope makes them one write, at the cost of reading each envelope's bytes when listing.
- **Parsing the header with a streaming decoder that stops at `header`**: cheaper on a large slot, but it would accept envelopes `Load` refuses, so a load screen could list a slot that cannot load. Using `parseEnvelope` keeps the two views the same.
- **A typed `SlotHeader` with `savedAt`**: the store has no clock, and a timestamp written on every save is the game's choice, so the header stays a game-defined object.

## Consequences

- **Benefits**: load screens list summaries without unmarshaling saves, and a header can never describe a different save than the data beside it, including after an interrupted write.
- **Costs**: `Headers` still reads and scans each slot's whole envelope, so it is not constant-time on large saves. A build older than this change reads a header envelope as `ErrCorrupt` and falls back to the backup, so a game that may be downgraded must not start writing headers within one schema version; the docs say so.
- **Not delivered from #1733**: thumbnails, `SaveAsync`/`LoadAsync`/`Flush` and their benchmark remain open.
