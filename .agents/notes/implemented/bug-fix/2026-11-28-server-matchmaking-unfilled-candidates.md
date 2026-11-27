# Agent Note: Preserve candidate tickets when an anchor room cannot fill in default matchmaking

Status: implemented

## Problem

`defaultRule` in `pkg/online/server/matchmaking.go` walked waiting tickets oldest-first and, for each anchor, mutated `remaining` in place by removing every candidate ticket added to the tentative `room` (`remaining = append(remaining[:j], remaining[j+1:]...)`). When the anchor could not fill `roomSize` players (`count != roomSize`), the rule advanced to the next anchor without restoring those removed candidates (#1600).

As a result, any later anchor in the same `Match()` call could never see tickets that an earlier unfilled anchor had tentatively considered. With `RoomSize: 3`, `BaseWindow: 100`, `WindowGrowthPerSecond: 0`, and ratings `A=0, B=90, C=180, D=170`, anchor `A` tentatively took `B` and failed to fill, dropping `B` from `remaining` so `B`, `C`, and `D` never formed a room even though all three fit within each other's window. The same drop also broke equal-rating party packing (for example, `RoomSize: 4` with parties of sizes `2, 3, 1` where the size-2 anchor tentatively took the solo and hid it from the size-3 party).

## Decision

- `defaultRule` records candidate indices (`chosen`) and pointers (`room`) for an anchor without mutating `remaining` during the candidate scan.
- When `count != roomSize`, `remaining` is untouched and the outer loop advances `i++`, leaving every candidate available to subsequent anchors in its original oldest-first `(EnqueuedAt, ID)` order.
- Only when `count == roomSize` does `defaultRule` emit the `Matchup` and compact `remaining` in a single forward pass starting at `i` (skipping the strictly increasing indices in `chosen` and clearing trailing pointers), leaving `i` at the next unvisited ticket.
- Candidate scanning stays forward-only (`j := i + 1`) so each formed room's anchor remains its oldest ticket and every member's rating difference from the room's oldest ticket satisfies `min(window(anchor), window(t))`.
- Empty-party anchors (`len(anchor.Party) == 0`) are skipped alongside oversized anchors (`len(anchor.Party) > roomSize`) for symmetry with the candidate check.

## Alternatives considered

- **Restore removed candidates into `remaining` on failure (snapshot or re-insert)**: preserves the original inner-loop mutation and rolls back when `count != roomSize`. Rejected because copying or re-inserting slices on every failed anchor allocates unnecessarily and is more error-prone than deferring deletion until commit.
- **Allow later anchors to scan backwards (`j < i`) to pair with earlier failed anchors**: could sometimes match a newer mid-rating ticket with both an older and a newer neighbor. Rejected because `defaultRule`'s documented contract requires every ticket in a room to fit within the room's oldest ticket's window; scanning backwards would break that invariant and violate oldest-first anchor semantics.

## Consequences

- **Benefits**: Unfilled anchors no longer hide compatible tickets from later fillable rooms—fixing both rating-window starvation and party-size packing misses—while preserving exact oldest-first determinism, tie-breaks, and zero public API changes. Single-pass compaction from `i` removes all chosen tickets in O(len(remaining)) time instead of repeated slice shifts per candidate, and `clear(remaining[write:])` avoids retaining stale pointers.
- **Costs and known limits**: Failed anchors still do not backtrack across party-size combinations within a single anchor (greedy oldest-first fill per anchor is unchanged). `chosen` and `room` allocate two scratch slices per `defaultRule` call, bounded by `min(roomSize, len(remaining))`.

## Testing

- `pkg/online/server/matchmaking_test.go`: added `TestMatchmakerUnfilledAnchorPreservesCandidates` (table-driven regression suite covering the #1600 rating-window case, parties of 2, multiple tentatively taken candidates, equal-rating party-size packing, oversized anchors, full-room party behind an unfilled anchor, multiple fillable rooms after unfilled anchors, mixed modes/regions, equal-timestamp ID tie-breaks, and asymmetric window growth) alongside existing unchanged matchmaking tests.
- Verified with `gofmt -l pkg/online/server/`, `go tool nsgo test ./pkg/online/server/...`, `go tool nsgo check -target macos_arm64,linux_x64,windows_x64 -jobs 2 ./pkg/online/server`, `(cd tools && go run ./changelog check)`, and `bun run verify-notes`.
