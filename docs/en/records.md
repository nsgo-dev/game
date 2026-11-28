# records

`github.com/nsgo-dev/game/pkg/online/records` provides a server-authoritative JSON record boundary. Records are keyed by owner, collection and key, carry monotonically increasing versions and can be applied in one compare-and-set transaction. `PublicRead` records can be listed by other players, while owner records stay private.

`Store.Apply` validates every expected version before mutating any record. `Expected: 0` requires a new record and a positive expected version performs a conditional update. A conflict returns `ErrChanged` and leaves the whole transaction unchanged. The in-memory core is intended for persistent SQLite/Postgres adapters and wallet or match-result transactions.
