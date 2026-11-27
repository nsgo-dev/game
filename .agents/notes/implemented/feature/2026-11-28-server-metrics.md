# Agent Note: Per-match tick histograms and guard and inbox counters on /metrics

Status: implemented

## Problem

`/metrics` exported five process-wide series and a `nsgo_server_tick_duration_seconds` gauge holding only the last observed tick. With several matches per process the gauge was overwritten by whichever match reported last, and a 15 s scrape saw one tick out of ~900 at 60 Hz, so spikes were invisible. The data for better metrics already existed but was not exported: `Runner.OnOverrun` reported each overrun only to a callback, `Guard.Stats` returned per-peer counters with no server-wide total, and every transport's `InboxReporter.InboxStats` went unread. A game also could not add its own series without replacing the handler.

## Decision

`Health` keeps a mutex-protected map of per-match accumulators (a fixed-bucket histogram plus overrun and missed-tick counters) and a slice of extra collectors, and `/metrics` renders them after the existing five series:

- `Health.ObserveMatchTick(match, d)` records one tick in `nsgo_server_match_tick_seconds{match}`, a histogram with fixed buckets from 0.5 ms to 100 ms. `Runner` gains an optional `Health` field and records every tick itself when set.
- `Health.OnOverrun(next)` returns a `Runner.OnOverrun` that counts `nsgo_server_match_overruns_total{match}` and `nsgo_server_match_missed_ticks_total{match}`, then calls `next`.
- `Health.AddCollector(func(io.Writer))` appends raw exposition text on every scrape. `GuardCollector(g)` exports `nsgo_server_guard_{packets,bytes,dropped,violations}_total` from the new `Guard.Totals()`, and `InboxCollector(name, transport)` exports `nsgo_net_inbox_{unreliable_dropped,reliable_refused,reliable_warnings}_total{transport}`.
- `Guard.Totals()` sums counters across every peer the guard has seen: `Reset` folds a forgotten peer's counters into an accumulator, and `PendingBytes` covers only the peers still tracked, since it is a gauge of current reservations, not a counter. `Guard` is now safe for concurrent use (a mutex around its public methods), because a collector reads `Totals` from the HTTP handler while the packet loop checks packets.
- An unnamed match becomes `match_<index>`: `Runner.Run` fills `Match.Name` before the goroutines start, so the histogram, the overrun counters and the overrun log reports all use the same label. Match names are escaped as label values (`\`, `"` and newline).
- The text format is still written by hand; there is no Prometheus client dependency. The exposition-format contract is enforced by a strict parser written in `metrics_test.go` (HELP/TYPE discipline, label escaping, cumulative histogram buckets with a +Inf bucket equal to `_count`), which every scrape test runs over the full output.

The metric names and labels are an operator-facing contract; rename them only with a migration note. The existing `nsgo_server_tick_duration_seconds` gauge stays for compatibility.

## Alternatives considered

- **A Prometheus client dependency** — client_golang would give the exposition format, escaping and histogram types for free, but it is a large dependency for one endpoint, and the package already wrote the text format by hand; the issue rules it out. The strict parser in the test covers the format mistakes a client would have prevented.
- **Recording overruns automatically from `Runner.Health` instead of the `Health.OnOverrun` wrapper** — one field would enable everything, but the runner would then always count overruns into the health even when the game wants its own handling only; the issue proposes the wrapper, and composing `health.OnOverrun(r.OnOverrun)` keeps the callback chain explicit.
- **Computing guard totals on demand by summing live peers only** — peers forgotten by `Reset` (reconnects, kicks) would vanish from the totals and the counters would no longer grow monotonically, breaking Prometheus rate queries; the accumulator in `Reset` keeps them monotonic.

## Consequences

- **Benefits**: tick-time distributions and overrun counts per match come out of the box, guard abuse and inbox backpressure are visible without per-game work, and a game appends its own series without replacing the handler.
- **Costs and known limits**: `Guard` calls now serialize on its mutex, negligible next to the token-bucket work. `Runner.Run` rewrites an empty `Match.Name` to `match_<index>`, which is visible to the game's own `OnOverrun`. The histogram's buckets are fixed, so a match slower than 100 ms per tick lands only in `+Inf`; bucket boundaries cannot be configured without a code change. The strict exposition parser lives in the test package, so other packages that hand-write metrics cannot reuse it.
- Verification: `go tool nsgo check` and `go tool nsgo test` on `pkg/online/server`, including synctest-driven overrun tests and the strict parser over every scrape.
